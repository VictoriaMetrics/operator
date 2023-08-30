# VMStaticScrape

The `VMStaticScrape` CRD provides mechanism for scraping metrics from static targets, configured by CRD targets.
By specifying configuration at CRD, operator generates config for `VMAgent` and syncs it. It's useful for external targets management,
when service-discovery is not available. `VMAgent` staticScrapeSelector must match `VMStaticScrape` labels.
See more details about selectors [here](https://docs.victoriametrics.com/operator/quick-start.html#object-selectors).

## Specification

You can see the full actual specification of the `VMStaticScrape` resource in
the [API docs -> VMStaticScrape](https://docs.victoriametrics.com/operator/api.html#vmstaticscrape).

**TODO**
