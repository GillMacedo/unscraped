package k8s

import "k8s.io/apimachinery/pkg/labels"

func labelsSet(m map[string]string) labels.Set { return labels.Set(m) }
