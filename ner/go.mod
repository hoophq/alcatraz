// This is a separate module so the alcatraz core stays dependency-free: the
// hugot model runtime is only pulled in if you import this package.
module github.com/hoophq/alcatraz/ner

go 1.26

require github.com/hoophq/alcatraz v0.12.0

require (
	github.com/gomlx/go-huggingface v0.3.5
	github.com/knights-analytics/hugot v0.7.5
	golang.org/x/text v0.37.0
)

require (
	github.com/daulet/tokenizers v1.27.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/gomlx/exceptions v0.0.3 // indirect
	github.com/gomlx/go-xla v0.2.2 // indirect
	github.com/gomlx/gomlx v0.27.3 // indirect
	github.com/gomlx/onnx-gomlx v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/knights-analytics/ortgenai v0.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/viant/afs v1.30.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yalue/onnxruntime_go v1.31.0 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/exp v0.0.0-20260603202125-055de637280b // indirect
	golang.org/x/image v0.41.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/term v0.43.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
)
