all:
	make build
	make zip

build:
	GO111MODULE=off go get github.com/karalabe/xgo
	rm -rf dist
	mkdir dist
	cd dist && xgo --targets=linux/amd64,darwin/amd64,windows/amd64 github.com/pocke/korat-go

zip:
	cd dist && mv korat-go-linux-amd64 korat-go && tar zcvf korat-go-linux-amd64.tar.gz korat-go && rm korat-go -f
	cd dist && mv korat-go-darwin-10.6-amd64 korat-go && tar zcvf korat-go-darwin-10.6-amd64.tar.gz korat-go && rm korat-go -f
	cd dist && mv korat-go-windows-4.0-amd64.exe korat-go.exe && zip korat-go-windows-4.0-amd64.zip korat-go.exe && rm korat-go.exe -f
