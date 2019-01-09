build:
	GO111MODULE=off go get github.com/karalabe/xgo
	test -d dist || mkdir dist
	cd dist && xgo --targets=linux/amd64,darwin/amd64,windows/amd64 github.com/pocke/korat-go
