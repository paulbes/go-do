package main

import (
	"fmt"
	"io"
	"os"

	"github.com/paulbes/go-do/do"
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
