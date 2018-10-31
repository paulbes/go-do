package do

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"reflect"
	"regexp"
	"runtime"
	"strings"
)

const temporaryFilePrefix = "godo-temporary-file"

// StageFn provides the signature for runnable segments. The developer
// can provide their own stages if they so wish to.
type StageFn func(input interface{}, progress io.Writer) (output interface{}, err error)

// Run will execute the provided pipeline in the order defined, the output
// of one stage is forwarded to the following stage, where the last
// result is returned, unless an error occurs somewhere during execution.
// The progress of the pipeline can be followed by providing a writer.
func Run(progress io.Writer, stages ...StageFn) (input interface{}, err error) {
	if progress == nil {
		progress = ioutil.Discard
	}

	vars := map[string]interface{}{}
	var closeFiles []*os.File
	var removeTempFiles []*os.File
ToExecution:
	for _, stageFn := range stages {
		fnName := runtime.FuncForPC(reflect.ValueOf(stageFn).Pointer()).Name()
		if strings.Contains(fnName, runtime.FuncForPC(reflect.ValueOf(Exec).Pointer()).Name()) {
			input = interceptExec{
				Input: input,
				Vars:  vars,
			}
		}
		if input, err = stageFn(input, progress); err != nil {
			break
		}
		switch f := input.(type) {
		case *os.File:
			if strings.HasPrefix(path.Base(f.Name()), temporaryFilePrefix) {
				removeTempFiles = append(removeTempFiles, f)
			} else {
				closeFiles = append(closeFiles, f)
			}
		case save:
			if _, hasKey := vars[f.Var]; hasKey {
				err = fmt.Errorf("variable: %s already exists", f.Var)
				break ToExecution
			}
			vars[f.Var] = f.Val
		}
	}
	for _, f := range closeFiles {
		err = f.Close()
		if err != nil {
			return
		}
	}
	for _, f := range removeTempFiles {
		err = os.Remove(f.Name())
		if err != nil {
			return
		}
	}
	return
}

type save struct {
	Var string
	Val interface{}
}

// SaveInVar allows you to save the output of a proceeding stage in a variable
// and reference it as #{varName} for future usage in any Exec stage. The
// variable name will not be evaluated until run-time, it
// must conform to [a-zA-Z], and not match the default `content` or `file`
// variables, as these are used for simple variable referencing of the
// provided input of the previous stage.
func SaveInVar(varName string) StageFn {
	return func(input interface{}, progress io.Writer) (output interface{}, err error) {
		valid, err := regexp.Match("^[a-zA-Z]+$", []byte(varName))
		if err != nil {
			return nil, err
		}
		if varName == "content" || varName == "file" || !valid {
			return nil, fmt.Errorf("not a valid variable name, must match: [a-zA-Z] (excluding: content, file)")
		}
		return save{
			Var: varName,
			Val: input,
		}, nil
	}
}

// MarshalJSON will serialise the input struct as JSON
func MarshalJSON(input interface{}, progress io.Writer) (interface{}, error) {
	ReportProgress(progress, "Marshalling provided content as JSON")
	return json.Marshal(input)
}

// UnmarshalJSON will unmarshal the JSON data to the provided interface{}
func UnmarshalJSON(to interface{}) StageFn {
	return func(input interface{}, progress io.Writer) (interface{}, error) {
		ReportProgress(progress, "Unmarshalling provided JSON data into struct")
		var content []byte
		switch data := input.(type) {
		case string:
			content = []byte(data)
		case []byte:
			content = data
		default:
			return nil, fmt.Errorf("provided input must be string or []byte")
		}

		err := json.Unmarshal(content, to)
		return to, err
	}
}

// ReportProgress simply converts the string to []byte and writes it to the
// progress stream
func ReportProgress(progress io.Writer, msg string, args ...interface{}) {
	if progress != nil {
		msg = fmt.Sprintf(msg, args...)
		_, _ = progress.Write([]byte(fmt.Sprintf("\n%s\n", msg)))
	}
}

// WriteFile permanently to a provided output file
func WriteFile(toFile string) StageFn {
	return func(input interface{}, progress io.Writer) (output interface{}, err error) {
		var content []byte
		switch data := input.(type) {
		case string:
			content = []byte(data)
		case []byte:
			content = data
		default:
			return nil, fmt.Errorf("provided input must be string or []byte")
		}

		err = ioutil.WriteFile(toFile, content, 0666)
		if err != nil {
			return input, err
		}

		return os.Open(toFile)
	}
}

// LoadFileHandler opens an *os.File handler to the provided file. This discards
// the content of the previous stage.
func LoadFileHandler(name string, flag int, perm os.FileMode) StageFn {
	return func(input interface{}, progress io.Writer) (output interface{}, err error) {
		ReportProgress(progress, "Loading file handler to: %s", name)
		return os.OpenFile(name, flag, perm)
	}
}

// ReadFile loads the content of a file, warning, this discards
// the content of the previous stage.
func ReadFile(fromFile string) StageFn {
	return func(_ interface{}, progress io.Writer) (interface{}, error) {
		ReportProgress(progress, "Reading content of file: %s", fromFile)
		return ioutil.ReadFile(fromFile)
	}
}

// WriteTempFile the content of the previous stage to a temporary file and return the
// filename
func WriteTempFile(input interface{}, progress io.Writer) (_ interface{}, err error) {
	var content []byte
	switch data := input.(type) {
	case string:
		content = []byte(data)
	case []byte:
		content = data
	default:
		return nil, fmt.Errorf("provided input must be string or []byte")
	}

	var f *os.File
	if f, err = ioutil.TempFile("", temporaryFilePrefix); err != nil {
		return nil, err
	}
	ReportProgress(progress, fmt.Sprintf("Created temporary file: %s", f.Name()))

	if _, err := f.Write(content); err != nil {
		return nil, err
	}
	ReportProgress(progress, "Content written to temporary file.")

	if err := f.Close(); err != nil {
		return nil, err
	}

	return f, nil
}

// SplitResult contains the result of splitting the pipeline
type SplitResult struct {
	Left  interface{}
	Right interface{}
}

// Split the preceding stages output and pipe it to two distinct paths
// where the left path is completed first, followed by the right path
// if any of the pipelines error, return the error instead
func Split(left, right []StageFn) StageFn {
	return func(input interface{}, progress io.Writer) (output interface{}, err error) {
		l, err := Run(progress, append([]StageFn{Insert(input)}, left...)...)
		if err != nil {
			return input, err
		}
		r, err := Run(progress, append([]StageFn{Insert(input)}, right...)...)
		if err != nil {
			return input, err
		}
		return SplitResult{Left: l, Right: r}, nil
	}
}

// Insert a given value into the pipeline, this can be nil for example
func Insert(val interface{}) StageFn {
	return func(_ interface{}, progress io.Writer) (interface{}, error) {
		ReportProgress(progress, "Inserting value into pipeline")
		return val, nil
	}
}

type interceptExec struct {
	Input interface{}
	Vars  map[string]interface{}
}

func replaceVar(cmd, varName string, with interface{}) (string, error) {
	var content string
	switch data := with.(type) {
	case []byte:
		content = string(data)
	case string:
		content = data
	case *os.File:
		content = data.Name()
	default:
		return "", fmt.Errorf("don't know how to replace content, required: string, []byte or *os.File")
	}
	return strings.Replace(cmd, fmt.Sprintf("#{%s}", varName), content, -1), nil
}

// Exec runs a command given the provided input, if the previous stage
// returns an *os.File the command can contain a #{file} that will replace
// this variable with the given file name. If the previous stage returns
// a string or []byte, the data can be injected into this command by using
// the #{content} placeholder.
func Exec(cmd string) StageFn {
	return func(input interface{}, progress io.Writer) (output interface{}, err error) {
		switch data := input.(type) {
		case interceptExec:
			switch d := data.Input.(type) {
			case []byte:
				cmd = strings.Replace(cmd, "#{content}", string(d), -1)
			case string:
				cmd = strings.Replace(cmd, "#{content}", d, -1)
			case *os.File:
				cmd = strings.Replace(cmd, "#{file}", d.Name(), -1)
			}
			for varName, i := range data.Vars {
				cmd, err = replaceVar(cmd, varName, i)
				if err != nil {
					return nil, err
				}
			}
		default:
			// Should never reach this point
			return nil, fmt.Errorf("exec command wasn't intercepted")
		}
		ReportProgress(progress, fmt.Sprintf("Executing command: %s", cmd))
		return doExecute(progress, cmd)
	}
}

func doExecute(progress io.Writer, command string) (interface{}, error) {
	var errOut, errErr error

	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	//FIXME: should resolve shell
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = wd
	stdoutIn, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrIn, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	var errBuff, outBuff bytes.Buffer
	stdout := io.MultiWriter(progress, &outBuff)
	stderr := io.MultiWriter(progress, &errBuff)

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	go func() {
		_, errOut = io.Copy(stdout, stdoutIn)
	}()

	go func() {
		_, errErr = io.Copy(stderr, stderrIn)
	}()

	err = cmd.Wait()
	if err != nil {
		return nil, err
	}

	if errOut != nil || errErr != nil {
		return nil, err
	}

	return outBuff.Bytes(), nil
}

// ExcludeLines will remove any lines in the input data containing
// any of the provided items.
func ExcludeLines(separator string, exclusions ...string) StageFn {
	return func(input interface{}, progress io.Writer) (output interface{}, err error) {
		var content []string
		switch data := input.(type) {
		case []byte:
			content = strings.Split(string(data), separator)
		case string:
			content = strings.Split(data, separator)
		}
		var out []string
	ToNextLine:
		for _, line := range content {
			for _, exclude := range exclusions {
				if strings.Contains(line, exclude) {
					continue ToNextLine
				}
			}
			out = append(out, line)
		}
		return strings.Join(out, separator), nil
	}
}
