# Infra design model

This isolated Go module accompanies [DESIGN.md](../../DESIGN.md). It demonstrates the proposed generic API and an Infra rule set, using only the Go standard library and Kubernetes API machinery. It does not define the repository's eventual public package.

Run `go test ./...` from this directory. The public corpus contains 27 aggregation fixtures and 17 summary fixtures, plus checks for generic rules, composition, recovery, polarity, error atomicity, and change reporting.

`model.go` intentionally keeps the evaluator and application implementations together for design review. The production layout described in `DESIGN.md` separates the root package from external-package examples. Native message format, generic freshness/validity, bounded working memory, and full reference-integration conformance remain open.
