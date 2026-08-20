// This is a separate module so the alcatraz core stays dependency-free: the
// purego FFI layer (and, at runtime, the privacy-filter.cpp shared library)
// is only pulled in if you import this package.
module github.com/hoophq/alcatraz/pfilter

go 1.24

require github.com/hoophq/alcatraz v0.20.2

require github.com/ebitengine/purego v0.9.1
