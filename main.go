package main

import "fmt"

func main() {
	chs, err := SelectChannels()
	if err != nil {
		panic(err)
	}
	for _, c := range chs {
		fmt.Printf("%+v\n", c)
	}
}
