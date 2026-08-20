// This is a separate module so the alcatraz core stays dependency-free: the
// hugot model runtime is only pulled in if you import this package.
module github.com/hoophq/alcatraz/ner

go 1.26.5

require github.com/hoophq/alcatraz v0.20.2

require (
	github.com/gomlx/go-huggingface v0.4.1
	github.com/knights-analytics/hugot v0.7.7
	golang.org/x/text v0.40.0
)

require (
	github.com/daulet/tokenizers v1.27.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/gomlx/compute v0.1.2 // indirect
	github.com/gomlx/exceptions v0.0.3 // indirect
	github.com/gomlx/go-xla v0.4.1 // indirect
	github.com/gomlx/gomlx v0.28.2 // indirect
	github.com/gomlx/onnx-gomlx v0.5.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/knights-analytics/ortgenai v0.3.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/viant/afs v1.30.0 // indirect
	github.com/yalue/onnxruntime_go v1.32.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/exp v0.0.0-20260727155853-b88d891fe743 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
)
