package do

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

type Test struct {
	Name string `json:"name"`
}

func GetName(input interface{}, _ io.Writer) (interface{}, error) {
	switch t := input.(type) {
	case *Test:
		return t.Name, nil
	}
	return nil, nil
}

func TestRun(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	defer func() {
		_ = os.RemoveAll(dir)
	}()
	assert.Nil(t, err)

	testCases := []struct {
		name        string
		stages      []StageFn
		expect      interface{}
		expectError bool
	}{
		{
			name: "exec",
			stages: []StageFn{
				Exec(`echo -n "hello there"`),
			},
			expect:      []byte("hello there"),
			expectError: false,
		},
		{
			name: "exec error",
			stages: []StageFn{
				Exec(`ech -n "hello there"`),
			},
			expect:      fmt.Errorf("exit status 127"),
			expectError: true,
		},
		{
			name: "temp write",
			stages: []StageFn{
				Exec(`echo -n "hello there"`),
				WriteTempFile,
				Exec(`cat #{file}`),
			},
			expect:      []byte("hello there"),
			expectError: false,
		},
		{
			name: "pipe",
			stages: []StageFn{
				Exec(`echo -n "hello there"`),
				Exec(`echo -n "#{content}"`),
			},
			expect:      []byte("hello there"),
			expectError: false,
		},
		{
			name: "JSON marshalling",
			stages: []StageFn{
				Exec(`echo -n "{\"name\": \"bob\"}"`),
				UnmarshalJSON(&Test{}),
				MarshalJSON,
			},
			expect:      []byte(`{"name":"bob"}`),
			expectError: false,
		},
		{
			name: "JSON unmarshalling error",
			stages: []StageFn{
				Exec(`echo -n "\"name\": \"bob\"}"`),
				UnmarshalJSON(&Test{}),
			},
			expect:      fmt.Errorf("invalid character ':' after top-level value"),
			expectError: true,
		},
		{
			name: "exclude",
			stages: []StageFn{
				Exec(`echo -e "hi\nthere and here\nyou"`),
				ExcludeLines("\n", "hi", "there"),
			},
			expect:      "you\n",
			expectError: false,
		},
		{
			name: "exclude string",
			stages: []StageFn{
				Insert("hello everyone"),
				ExcludeLines("\n", "every"),
			},
			expect:      "",
			expectError: false,
		},
		{
			name: "split",
			stages: []StageFn{
				Exec(`echo -n "{\"name\": \"bob\"}"`),
				Split(
					[]StageFn{UnmarshalJSON(&Test{})},
					[]StageFn{UnmarshalJSON(&Test{}), GetName},
				),
			},
			expect: SplitResult{
				Left:  &Test{Name: "bob"},
				Right: "bob",
			},
			expectError: false,
		},
		{
			name: "read/write",
			stages: []StageFn{
				Insert("some content"),
				WriteFile(path.Join(dir, "something")),
				ReadFile(path.Join(dir, "something")),
			},
			expect:      []byte("some content"),
			expectError: false,
		},
		{
			name: "save/exec",
			stages: []StageFn{
				Insert("hello"),
				SaveInVar("myVar"),
				Insert(nil),
				Exec(`echo -n "#{myVar}"`),
			},
			expect:      []byte("hello"),
			expectError: false,
		},
		{
			name: "save illegal var",
			stages: []StageFn{
				Insert("hello"),
				SaveInVar("myVar1"),
			},
			expect:      fmt.Errorf("not a valid variable name, must match: [a-zA-Z] (excluding: content, file)"),
			expectError: true,
		},
		{
			name: "save file name",
			stages: []StageFn{
				Insert("hello"),
				WriteTempFile,
				SaveInVar("myFile"),
				Insert(nil),
				Exec("cat #{myFile}"),
			},
			expect:      []byte("hello"),
			expectError: false,
		},
		{
			name: "duplicate var",
			stages: []StageFn{
				Insert("hello"),
				SaveInVar("myVar"),
				SaveInVar("myVar"),
			},
			expect:      fmt.Errorf("variable: myVar already exists"),
			expectError: true,
		},
		{
			name: "filer handler loading",
			stages: []StageFn{
				Insert("hi there"),
				WriteFile(path.Join(dir, "test")),
				LoadFileHandler(path.Join(dir, "test"), os.O_RDONLY, 0666),
				Exec("cat #{file}"),
			},
			expect:      []byte("hi there"),
			expectError: false,
		},
	}

	for _, tc := range testCases {
		got, err := Run(nil, tc.stages...)
		if tc.expectError {
			assert.Equal(t, tc.expect.(error).Error(), err.Error(), tc.name)
		} else {
			assert.Equal(t, tc.expect, got, tc.name)
			assert.Nil(t, err, tc.name)
		}
	}
}
