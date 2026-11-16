# 3. Findings only when the inputs settle it

Date: 2026-11-16

## Decision
A finding is reported only when the objects in the inputs decide it. When a link depends on something that isn't in the files, such as pods created by an operator, a Service in another chart, or labels of a Namespace that wasn't passed, unscraped lists the link as not checked, with the reason, and doesn't count it as a finding or as healthy.

A selector that matches nothing in the inputs is a finding only when the inputs show the likely mistake: a matching Service or pod in another namespace, a Service whose pod selector matches, or Service labels on a PodMonitor.

## Why
Rendered charts rarely hold everything. kube-prometheus-stack's own ServiceMonitors point at the kubelet Service, which the operator creates, and at Prometheus pods, which the operator creates. Reporting those as broken would teach people to ignore the tool. The real charts in `tests/testdata/real` keep this honest: they must stay quiet.
