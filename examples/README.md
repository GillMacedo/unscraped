# Examples

- `broken/shop.yaml`: a made-up shop with one Prometheus, three Namespaces and four services. Each monitor and the rule has one mistake, explained in a comment above it. unscraped reports six findings and exits 1.
- `fixed/shop.yaml`: the same file with each mistake fixed. unscraped is quiet and exits 0.

The service names, images and labels are invented.
