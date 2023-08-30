# VMProbe

The `VMProbe` CRD provides probing target ability with a prober. The most common prober is [blackbox exporter](https://github.com/prometheus/blackbox_exporter).
By specifying configuration at CRD, operator generates config for `VMAgent` and syncs it. It's possible to use static targets
or use standard k8s discovery mechanism with `Ingress`.
You have to configure blackbox exporter before you can use this feature. The second requirement is `VMAgent` selectors,
it must match your `VMProbe` by label or namespace selector. `VMAgent` probeSelector must match `VMProbe` labels.
See more details about selectors [here](https://docs.victoriametrics.com/operator/quick-start.html#object-selectors).

## Specification

You can see the full actual specification of the `VMProbe` resource in
the [API docs -> VMProbe](https://docs.victoriametrics.com/operator/api.html#vmprobe).

**TODO**
