subinclude("///pleasings2//go:compat")

go_library(
    name = "secret",
    srcs = glob(
        ["*.go"],
        exclude = ["*_test.go"],
    ),
    visibility = ["PUBLIC"],
    deps = [
        "//internal/secret/secrettype",
        "//pkg/providers/oracle/oci",
    ],
)
