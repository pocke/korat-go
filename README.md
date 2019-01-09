korat-go
===

The backend process for [korat](https://github.com/pocke).

It is responsible for searching and storing GitHub issues.


Development
---

```
$ GO111MODULE=on go get github.com/pocke/korat-go

# Setup database
$ korat-go       # To run migration
$ pkill korat-go # Kill the process after a second

# If you need, replace "pocke" with your GitHub account.
$ cd $GOPATH/src/github.com/pocke/korat-go
$ GITHUB_ACCESS_TOKEN=xxx ./dev_seed_data.sh

# start server
$ korat-go
```

Build binaries for each platform
---

```
$ make
```
