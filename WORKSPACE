workspace(name = "com_github_buildbarn_bb_autoscaler")

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

http_archive(
    name = "io_bazel_rules_docker",
    sha256 = "6287241e033d247e9da5ff705dd6ef526bac39ae82f3d17de1b69f8cb313f9cd",
    strip_prefix = "rules_docker-0.14.3",
    urls = ["https://github.com/bazelbuild/rules_docker/releases/download/v0.14.3/rules_docker-v0.14.3.tar.gz"],
)

http_archive(
    name = "io_bazel_rules_go",
    sha256 = "a8d6b1b354d371a646d2f7927319974e0f9e52f73a2452d2b3877118169eb6bb",
    urls = [
        "https://mirror.bazel.build/github.com/bazelbuild/rules_go/releases/download/v0.23.3/rules_go-v0.23.3.tar.gz",
        "https://github.com/bazelbuild/rules_go/releases/download/v0.23.3/rules_go-v0.23.3.tar.gz",
    ],
)

http_archive(
    name = "bazel_gazelle",
    sha256 = "cdb02a887a7187ea4d5a27452311a75ed8637379a1287d8eeb952138ea485f7d",
    urls = [
        "https://mirror.bazel.build/github.com/bazelbuild/bazel-gazelle/releases/download/v0.21.1/bazel-gazelle-v0.21.1.tar.gz",
        "https://github.com/bazelbuild/bazel-gazelle/releases/download/v0.21.1/bazel-gazelle-v0.21.1.tar.gz",
    ],
)

load("@bazel_tools//tools/build_defs/repo:git.bzl", "git_repository")

git_repository(
    name = "com_github_buildbarn_bb_storage",
    commit = "0faeb793e9e00ba80b94f40132f7e8dd90bd0036",
    remote = "https://github.com/buildbarn/bb-storage.git",
)

load("@io_bazel_rules_docker//repositories:repositories.bzl", container_repositories = "repositories")

container_repositories()

load("@io_bazel_rules_go//go:deps.bzl", "go_rules_dependencies", "go_register_toolchains")

go_rules_dependencies()

go_register_toolchains()

load("@bazel_gazelle//:deps.bzl", "gazelle_dependencies")

gazelle_dependencies()

load("@io_bazel_rules_docker//go:image.bzl", _go_image_repos = "repositories")

_go_image_repos()

load("//:go_dependencies.bzl", "bb_autoscaler_go_dependencies")

bb_autoscaler_go_dependencies()

# TODO: This refers to a copy of go_dependencies.bzl that is manually
# copied from the bb-storage repository. This is a requirement to make
# "gazelle:repository_macro" work.
# Details: https://github.com/bazelbuild/bazel-gazelle/issues/752
# gazelle:repository_macro go_dependencies_bb_storage.bzl%bb_storage_go_dependencies
load(":go_dependencies_bb_storage.bzl", "bb_storage_go_dependencies")

bb_storage_go_dependencies()

load("@com_github_bazelbuild_remote_apis//:repository_rules.bzl", "switched_rules_by_language")

switched_rules_by_language(
    name = "bazel_remote_apis_imports",
    go = True,
)

http_archive(
    name = "com_google_protobuf",
    sha256 = "761bfffc7d53cd01514fa237ca0d3aba5a3cfd8832a71808c0ccc447174fd0da",
    strip_prefix = "protobuf-3.11.1",
    urls = ["https://github.com/protocolbuffers/protobuf/releases/download/v3.11.1/protobuf-all-3.11.1.tar.gz"],
)

load("@com_google_protobuf//:protobuf_deps.bzl", "protobuf_deps")

protobuf_deps()
