.PHONY: test test-k8s lint demo

test:
	test -z "$$(gofmt -l plugin tests)"
	go vet ./...
	go test -race ./...

# Compares label selector matching with k8s.io/apimachinery. Needs network the first time.
test-k8s:
	cd tests/k8s && go vet ./... && go test ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

# The broken example exits 1 on purpose. The fixed one and the real charts
# must stay quiet.
demo:
	go run ./plugin/scripts examples/broken/shop.yaml || test $$? -eq 1
	go run ./plugin/scripts examples/fixed/shop.yaml
	go run ./plugin/scripts tests/testdata/real/kube-prometheus-stack/rendered.yaml tests/testdata/real/ingress-nginx/rendered-release-label.yaml
	go run ./plugin/scripts -n flux-system tests/testdata/real/kube-prometheus-stack/prometheus-select-all.yaml tests/testdata/real/ingress-nginx/rendered.yaml tests/testdata/real/cert-manager/rendered.yaml tests/testdata/real/argo-cd/rendered.yaml tests/testdata/real/flux2/rendered.yaml
