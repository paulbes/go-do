# godo

Go, do! Simplify execution of command line operations and their intersection with a proper programming language. (Bash who?)

<img src="https://github.com/paulbes/go-do/raw/master/media/dodo.jpeg" width="150" align="middle" alt="You can godo it!">

## Important: refactoring or adding new functions

When adding a new function it should always be in the form of: *ActionTarget* or *ActionSource*, so that it is clear what the effect of applying the stage is and what it is acting on.

Existing functions should never be removed, to ensure backwards compatibility, unless good notice has been given.

## Variables

It is possible to save the output of a preceding stage using `SaveInVar("myVarName")`, to be referenced in later `Exec` stages using `#{myVarName}` in the command string, e.g., `Exec("echo -n "#{myVarName}")`.

The variable name can be reused in multiple `Exec` stages, and **all occurrences** referencing a variable will be substituted.

### Content and file variables

There are two special variables: `#{content}` and `#{file}` that are made available for `Exec` under certain conditions:

- content
  - Is available when the preceding stage outputs a `string` or `[]byte` and will replace the `#{content}` with that output; this variable can be referenced multiple times.
- file
  - Is available when the preceding stages outputs an `*os.File` and will replace the `#{file}` with the name of the `*os.File`; this variable can be referenced multiple times.

## Usage

```bash
go get github.com/paulbes/go-do/do
```

### Example

The following code demonstrates how you can save some data to a variable (for later referencing), inject a struct into the pipeline, serialise it to json, and write the content to a temporary file. Finally, we execute a command that prints out our saved variable and cats the content of the temporary file.

```go
package main

import (
	"fmt"
	"github.com/paulbes/go-do/do"
	"io"
	"os"
)

// GreetingSubject is used for demonstrating json serialisation
type GreetingSubject struct {
	Name string `json:"name"`
}

func main() {
	// We ignore the result and error here, but the result
	// will contain the data of the last executed stage
	_, _ = do.Run(os.Stdout,
		do.Insert("hello"),
		do.SaveInVar("greeting"),
		do.Insert(GreetingSubject{Name: "bob"}),
		do.MarshalJSON,
		do.WriteTempFile,
		do.Exec(`echo -n "#{greeting}" && cat #{file}`),
		func(input interface{}, _ io.Writer) (interface{}, error) {
			switch d := input.(type) {
			case []byte:
				fmt.Println("\nGot output: " + string(d))
			}
			return nil, nil
		},
	)
}
```

## Functions

| Function | Returns | Description | Notable side-effects
|---|---|---|---|
|SaveInVar(varName)|Output from previous stage|Save the content of the previous stage to var `varName`| None |
|MarshalJSON|[]byte|Marshal input as JSON| None |
|UnmarshalJSON(to interface{})|to interface{}|Unmarshal output of previous stage as JSON into `to` | None |
|WriteFile(fileName)|*os.File|Write content of previous stage to a permanent file| File will not be removed after pipeline completion |
|LoadFileHandler(fileName, flag, perm)|*os.File|Opens the provided `fileName` for reading | Discards the output from the previous stage |
|ReadFile(fileName)|[]byte|Reads the content from the provided file| Discards the output from the previous stage |
|WriteTempFile|*os.File|Creates a temporary file from the input of the previous stage with| File is removed after pipeline completion |
|Insert(i interface{})|i interface{}|Inserts the provided value into the pipeline | Discards the output from the previous stage |
|Exec(cmd)|[]byte|Executes the provided command|None|
|ExcludeLines(sep, exclusions)|string|Splits the content of the previous stage using the provided separator, removes all lines that match on the exclusions and returns a joined string using the provided separator|None|
