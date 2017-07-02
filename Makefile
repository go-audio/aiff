default: *.go
	go test -v ./...

install:
	go get -u github.com/go-audio/audio

testprof: *.go
	go test -cpuprofile cpu.prof
	go tool pprof -pdf aiff.test cpu.prof > cpu.pdf
	open cpu.pdf