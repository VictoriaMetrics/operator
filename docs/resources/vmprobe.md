# VMProbe

The `VMProbe` CRD provides probing target ability with some external prober. 
The most common prober is [blackbox exporter](https://github.com/prometheus/blackbox_exporter).
By specifying configuration at CRD, operator generates config for [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html)
and syncs it. It's possible to use static targets or use standard k8s discovery mechanism with `Ingress`.

`VMProbe` object generates part of [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html) configuration;
It has various options for scraping configuration of target (with basic auth, tls access, by specific port name etc.).

You have to configure blackbox exporter before you can use this feature. 
The second requirement is [VMAgent](https://docs.victoriametrics.com/operator/resources/vmagent.html) selectors,
it must match your `VMProbe` by label or namespace selector. `VMAgent` `probeSelector` must match `VMProbe` labels.

See more details about selectors [here](https://docs.victoriametrics.com/operator/resources/vmagent.html#scraping).

## Specification

You can see the full actual specification of the `VMProbe` resource in
the [API docs -> VMProbe](https://docs.victoriametrics.com/operator/api.html#vmprobe).

## Migration from Prometheus

The `VMProbe` CRD from VictoriaMetrics Operator is a drop-in replacement
for the Prometheus `Probe` from prometheus-operator.

More details about migration from prometheus-operator you can read in [this doc](https://docs.victoriametrics.com/operator/migration.html).

<!-- TODO: examples -->
