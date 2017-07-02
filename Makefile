default: *.go
	go test -v ./...

testprof: *.go
	go test -cpuprofile cpu.prof
	go tool pprof -pdf aiff.test cpu.prof > cpu.pdf
	open cpu.pdf