---
sort: 13
weight: 13
title: Custom resources
---

#  Custom resource kinds

This documentation section describes the design and interaction between the custom resource definitions (CRD) that the Victoria
Metrics Operator introduces.

[Operator]((https://docs.victoriametrics.com/vmoperator/)) introduces the
following [custom resources](https://docs.victoriametrics.com/vmoperator/#custom-resources):

- [VMAgent](https://docs.victoriametrics.com/vmoperator/resources/vmagent.html)
- [VMAlert](https://docs.victoriametrics.com/vmoperator/resources/vmalert.html)
- [VMAlertManager](https://docs.victoriametrics.com/vmoperator/resources/vmalertmanager.html)
- [VMAlertManagerConfig](https://docs.victoriametrics.com/vmoperator/resources/vmalertmanagerconfig.html)
- [VMAuth](https://docs.victoriametrics.com/vmoperator/resources/vmauth.html)
- [VMCluster](https://docs.victoriametrics.com/vmoperator/resources/vmcluster.html)
- [VMNodeScrape](https://docs.victoriametrics.com/vmoperator/resources/vmnodescrape.html)
- [VMPodScrape](https://docs.victoriametrics.com/vmoperator/resources/vmpodscrape.html)
- [VMProbe](https://docs.victoriametrics.com/vmoperator/resources/vmprobe.html)
- [VMRule](https://docs.victoriametrics.com/vmoperator/resources/vmrule.html)
- [VMServiceScrape](https://docs.victoriametrics.com/vmoperator/resources/vmservicescrape.html)
- [VMStaticScrape](https://docs.victoriametrics.com/vmoperator/resources/vmstaticscrape.html)
- [VMSingle](https://docs.victoriametrics.com/vmoperator/resources/vmsingle.html)
- [VMUser](https://docs.victoriametrics.com/vmoperator/resources/vmuser.html)

Here is the scheme of relations between the custom resources:

<img src="README_cr-relations.png" width="1000">

