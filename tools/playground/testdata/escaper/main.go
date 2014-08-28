package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
)

func main() {
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}
	out, err := json.Marshal(string(data))
	if err != nil {
		panic(err)
	}
	os.Stdout.Write(out)
}
