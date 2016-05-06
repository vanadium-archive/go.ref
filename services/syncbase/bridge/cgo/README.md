# Syncbase cgo bridge

The Syncbase cgo bridge exposes a C API to Syncbase. It's intended to be used on
iOS (Swift) and Android (Java), and perhaps elsewhere as well.

For the time being, we provide a `Makefile` to build C archives for all target
platforms. Eventually, we'll integrate C archive compilation with the `jiri`
tool.
