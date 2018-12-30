package main

func main() {
	err := StartFetchIssues()
	if err != nil {
		panic(err)
	}
	select {}
}
