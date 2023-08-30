# VMNodeScrape

The `VMNodeScrape` CRD provides discovery mechanism for scraping metrics kubernetes nodes.
By specifying configuration at CRD, operator generates config for `VMAgent` and syncs it. It's useful for cadvisor scraping,
node-exporter or other node-based exporters. `VMAgent` nodeScrapeSelector must match `VMNodeScrape` labels.
See more details about selectors [here](https://docs.victoriametrics.com/operator/quick-start.html#object-selectors).

## Specification

You can see the full actual specification of the `VMNodeScrape` resource in
the [API docs -> VMNodeScrape](https://docs.victoriametrics.com/operator/api.html#vmnodescrape).

**TODO**
