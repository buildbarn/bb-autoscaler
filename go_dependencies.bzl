load("@bazel_gazelle//:deps.bzl", "go_repository")

def bb_autoscaler_go_dependencies():
    go_repository(
        name = "com_github_json_iterator_go",
        importpath = "github.com/json-iterator/go",
        sha256 = "ca1fee8594ea5b4f41bce678c09a7b4b8300bf185701930cc5fcb1758e98dab1",
        strip_prefix = "go-1.1.9",
        urls = ["https://github.com/json-iterator/go/archive/v1.1.9.tar.gz"],
    )

    go_repository(
        name = "com_github_modern_go_concurrent",
        importpath = "github.com/modern-go/concurrent",
        sha256 = "dcc74b4158f0139506ad58e5ce9695c05b115aaa9011a2edacb492fc3d99def6",
        strip_prefix = "concurrent-1.0.3",
        urls = ["https://github.com/modern-go/concurrent/archive/1.0.3.tar.gz"],
    )

    go_repository(
        name = "com_github_modern_go_reflect2",
        importpath = "github.com/modern-go/reflect2",
        sha256 = "082935ada142b2e5fdbe6b12e276abb53c1f367444e48f0d576291b242ef4e11",
        strip_prefix = "reflect2-1.0.1",
        urls = ["https://github.com/modern-go/reflect2/archive/1.0.1.tar.gz"],
    )
