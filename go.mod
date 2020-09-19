module github.com/goldmann/cpaas-operator

go 1.13

require (
	github.com/go-logr/logr v0.1.0
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/tektoncd/pipeline v0.16.3
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.17.9
	k8s.io/apimachinery v0.17.9
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	sigs.k8s.io/controller-runtime v0.5.11
)

replace (
	k8s.io/client-go => k8s.io/client-go v0.17.9
	vbom.ml/util => github.com/fvbommel/util v0.0.0-20180919145318-efcd4e0f9787
)
