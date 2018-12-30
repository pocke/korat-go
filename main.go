package main

import "context"

func main() {
	ctx := context.Background()
	go func() {
		err := StartFetchIssues(ctx)
		if err != nil {
			panic(err)
		}
	}()
	go StartHTTPServer(5427)
	select {}
}
