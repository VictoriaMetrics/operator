---
weight: 10
title: CHANGELOG
menu:
  docs:
    parent: operator
    weight: 10
    identifier: operator-changelog
aliases:
  - /operator/changelog/
  - /operator/changelog/index.html
---

## tip

## [v0.68.7](https://github.com/VictoriaMetrics/operator/releases/tag/v0.68.7)
**Release date:** 27 Jul 2026

* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VM apps to [v1.147.0](https://github.com/VictoriaMetrics/VictoriaMetrics/releases/tag/v1.147.0) version
* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VT apps to [v0.9.4](https://github.com/VictoriaMetrics/VictoriaTraces/releases/tag/v0.9.4) version.
* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VMAnomaly to [v1.28.7](https://docs.victoriametrics.com/anomaly-detection/changelog/#v1287) version

* SECURITY: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/), [vmsingle](https://docs.victoriametrics.com/operator/resources/vmsingle/): remove cluster-wide `secrets` and `configmaps` permissions from the operator-managed `ClusterRole`. Secret access for the config-reloader is now granted via a namespace-scoped `Role` limited to the single operator-managed config secret. For `vmsingle` in ingest-only mode (the default), no secret or configmap permissions are granted at all.

* BUGFIX: [vmanomaly](https://docs.victoriametrics.com/operator/resources/vmanomaly/): add missing `scatter_infer_jobs` field to the periodic scheduler config struct. See [#2328](https://github.com/VictoriaMetrics/operator/issues/2328).
* BUGFIX: [vmanomaly](https://docs.victoriametrics.com/operator/resources/vmanomaly/): preserve insertion order of keys in `ProphetModel` `seasonalities`, `tz_seasonalities`, `compression`, and `args` fields; previously the operator re-emitted them with keys sorted alphabetically, which broke round-trips for configs that specified keys in a non-alphabetical order. Also renamed the singular `seasonality`/`tz_seasonality` YAML keys (deprecated) to the plural `seasonalities`/`tz_seasonalities` to match the vmanomaly configuration format. See [#2356](https://github.com/VictoriaMetrics/operator/issues/2356).
* BUGFIX: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/), [vmsingle](https://docs.victoriametrics.com/operator/resources/vmsingle/): create a `Role` and `RoleBinding` in each namespace listed in `WATCH_NAMESPACES` so that vmagent/vmsingle can perform service discovery in all watched namespaces, not only its own. Previously, in namespaced mode, vmagent/vmsingle could only scrape targets from its own namespace due to missing RBAC in other watched namespaces.
* BUGFIX: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): fix missing credential secret and config-reloader setup in `ingestOnlyMode` when remote write entries carry authentication secrets (`basicAuth.password`, `bearerTokenSecret`, or `oauth2.clientSecret`). Previously the operator-managed secret containing the credential files was never created in ingest-only mode, causing vmagent to start with dangling file references. The secret is now reconciled and the config-reloader is configured to watch it for credential rotation.

## [v0.68.6](https://github.com/VictoriaMetrics/operator/releases/tag/v0.68.6)
**Release date:** 25 Jun 2026

* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VM apps to [v1.146.0](https://github.com/VictoriaMetrics/VictoriaMetrics/releases/tag/v1.146.0) version
* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VL apps to [v1.51.0](https://github.com/VictoriaMetrics/VictoriaLogs/releases/tag/v1.51.0).
* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VT apps to [v0.9.3](https://github.com/VictoriaMetrics/VictoriaTraces/releases/tag/v0.9.3) version. 

* FEATURE: [vmoperator](https://docs.victoriametrics.com/operator/): add `victoriametrics_app=true` label to all metrics scraped by the operator. See [#2261](https://github.com/VictoriaMetrics/operator/issues/2261).

* BUGFIX: [config-reloader](https://docs.victoriametrics.com/operator/): fix possible panic on Secret watch events when the informer's local cache fell out of sync and Kubernetes delivered a stale tombstone entry instead of the Secret object. The config-reloader now unwraps tombstones correctly and logs an error for any other unexpected types.
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): update status currentRevision and currentReplicas for StatefulSet with OnDelete update strategy. See [#1242](https://github.com/VictoriaMetrics/operator/issues/1242).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): retry reconcile errors, that may lead to expanding state, before resource could hang in expanding state.
* BUGFIX: [vmcluster](https://docs.victoriametrics.com/operator/resources/vmcluster/), [vlcluster](https://docs.victoriametrics.com/operator/resources/vlcluster/) and [vtcluster](https://docs.victoriametrics.com/operator/resources/vtcluster/): when storage HPA was enabled, generated `-storageNode` flags could become incorrect after scaling, which could break expected routing to storage nodes; now the operator derives storage node count from the current StatefulSet state so generated flags stay correct during HPA-driven scaling. See [#2117](https://github.com/VictoriaMetrics/operator/issues/2117).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): update status currentRevision and currentReplicas for StatefulSet with OnDelete update strategy. See [#1242](https://github.com/VictoriaMetrics/operator/issues/1242).
* BUGFIX: [config-reloader](https://docs.victoriametrics.com/operator/): fix `configreloader_last_reload_success_timestamp_seconds` metric to report time in seconds instead of milliseconds.
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): ignore `NotFound` errors, that may occur during attempt to update status on a missing resource.
* BUGFIX: [vmanomaly](https://docs.victoriametrics.com/operator/resources/vmanomaly/): pass the configured TLS CA bundle to the reader, writer and monitoring clients. Previously the CA was mounted as a volume but dropped during config generation, so a `tlsConfig` with only a CA produced no `verify_tls` reference to it; `insecureSkipVerify` is now also propagated correctly.
* BUGFIX: [config-reloader](https://docs.victoriametrics.com/operator/): fix missed reload for watched files whose names contain `..` (e.g. `rules..yaml`). Previously any path containing `..` was silently skipped; now only Kubernetes synthetic entries whose basename starts with `..` (e.g. `..data`) are ignored. See [#2253](https://github.com/VictoriaMetrics/operator/pull/2253).

## [v0.68.5](https://github.com/VictoriaMetrics/operator/releases/tag/v0.68.5)
**Release date:** 27 May 2026

**Update note 1**: `-eula` flag is not set by default anymore for VMBackup and VMRestore. To avoid VMCluster/VMSingle rollouts set `spec.vmstorage.vmBackup.acceptEula: true` for VMCluster and `spec.vmBackup.acceptEula: true` for VMSingle and replace it with `spec.license` during VMSingle/VMCluster upgrade.

* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VM apps to [v1.144.0](https://github.com/VictoriaMetrics/VictoriaMetrics/releases/tag/v1.144.0) version

* SECURITY: upgrade Go builder from Go1.25.8 to Go1.25.10. See [the list of issues addressed in Go1.25.10](https://github.com/golang/go/issues?q=milestone%3AGo1.25.10+label%3ACherryPickApproved).

* BUGFIX: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): use volume from spec.volumes as persistent queue volume if its name is `persistent-queue-data`, previously emptyDir was mounted. See [#1677](https://github.com/VictoriaMetrics/operator/issues/1677).
* BUGFIX: [vmcluster](https://docs.victoriametrics.com/operator/resources/vmcluster/): use volume from spec.vmstorage.volumes and spec.vmselect.volumes as data and cache volumes if its name is `vmstorage-db` and `vmselect-cachedir` respectively. See [#784](https://github.com/VictoriaMetrics/operator/issues/784).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): Improve reconcile error handling for Prometheus and VictoriaMetrics controllers.
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): Add acceptEula support for VMBackup/VMRestore.
* BUGFIX: [vmdistributed](https://docs.victoriametrics.com/operator/resources/vmdistributed/): change default load balancing policy for write requests from `first_available` to `least_loaded`. This should allow to evenly distribute write load across all VMAgents.
* BUGFIX: [VMCluster](https://docs.victoriametrics.com/operator/resources/vmcluster/), [VTCluster](https://docs.victoriametrics.com/operator/resources/vtcluster/) and [VLCluster](https://docs.victoriametrics.com/operator/resources/vlcluster/): fixed infinite non-default additional service recreation, when requestsLoadBalancer.enabled: true
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): retry reconcile errors, that may lead to expanding state, before resource could hang in expanding state.
* BUGFIX: [vmdistributed](https://docs.victoriametrics.com/operator/resources/vmdistributed/): expose VMClusterSpec parsing error in status, previously it was just swallowed and led to infinite reconciles. See [#2113](https://github.com/VictoriaMetrics/operator/issues/2113).
* BUGFIX: [vmanomaly](https://docs.victoriametrics.com/operator/resources/vmanomaly/) and [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): Fix incorrect scaling logs for sharded vmagent and vmanomaly.

## [v0.68.4](https://github.com/VictoriaMetrics/operator/releases/tag/v0.68.4)
**Release date:** 09 Apr 2026

* FEATURE: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): introduce statefulRollingUpdateStrategyBehavior to allow managing VMAgent update strategy in a statefulMode. See [#1987](https://github.com/VictoriaMetrics/operator/issues/1987).
* FEATURE: [vmoperator](https://docs.victoriametrics.com/operator/): Dry-run mode. See [#1832](https://github.com/VictoriaMetrics/operator/issues/1832).

* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): wait till PVC resize finished. See [#1970](https://github.com/VictoriaMetrics/operator/issues/1970).
* BUGFIX: [vmalertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager/): fixed ignored tracing config, when no alertmanagerconfig CRs collected. See [#1983](https://github.com/VictoriaMetrics/operator/issues/1983).
* BUGFIX: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): apply scrape class relabellings before job ones. See [#1997](https://github.com/VictoriaMetrics/operator/issues/1997).
* BUGFIX: [vmanomaly](https://docs.victoriametrics.com/operator/resources/vmanomaly/) and [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): render %SHARD_NUM% placeholder when shard count is greater than 0. See [#2001](https://github.com/VictoriaMetrics/operator/issues/2001).
* BUGFIX: [vlcluster](https://docs.victoriametrics.com/operator/resources/vlcluster/) and [vtcluster](https://docs.victoriametrics.com/operator/resources/vtcluster/): do not ignore ExtraStorageNodes for select, when default storage is disabled. See [#1910](https://github.com/VictoriaMetrics/operator/issues/1910).
* BUGFIX: [vmdistributed](https://docs.victoriametrics.com/operator/resources/vmdistributed/): use default stub, when no VMAuth backends are available

## [v0.68.3](https://github.com/VictoriaMetrics/operator/releases/tag/v0.68.3)
**Release date:** 16 Mar 2026

* FEATURE: [vmoperator](https://docs.victoriametrics.com/operator/): prettify reconcile diff in logs, now diff objects show only changed JSON fields.
* FEATURE: [vmoperator](https://docs.victoriametrics.com/operator/): introduce VM_LOOPBACK env variable, which allows to override default loopback address, which is either `localhost` if `VM_ENABLETCP6=true` or `127.0.0.1` in other cases. Note: this change may cause component rollouts when 127.0.0.1 is used as the loopback address. Make sure to set this env var before upgrading.

* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): recreate STS if immutable fields changed.
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): pod recreation isn't lost after triggerring StatefulSet delete.
* BUGFIX: [vmdistributed](https://docs.victoriametrics.com/operator/resources/vmdistributed/): updated VMAuth config consolidating all VMSelects into a single read and all VMAgents into a single write backend.
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): fix finalizer cleanup for VLAgent, VLogs, VMAgent, and VMSingle to target the correct resources.
* BUGFIX: [config-reloader](https://github.com/VictoriaMetrics/operator/tree/master/cmd/config-reloader): `log-format` option supported as fallback for `loggerFormat`. See [#1954](https://github.com/VictoriaMetrics/operator/issues/1954).

* SECURITY: upgrade Go builder from Go1.25.7 to Go1.25.8. See [the list of issues addressed in Go1.25.8](https://github.com/golang/go/issues?q=milestone%3AGo1.25.8+label%3ACherryPickApproved).

## [v0.68.2](https://github.com/VictoriaMetrics/operator/releases/tag/v0.68.2)
**Release date:** 04 Mar 2026

* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VL apps to [v1.47.0](https://github.com/VictoriaMetrics/VictoriaLogs/releases/tag/v1.47.0).

* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): perform statefulset pods deletion instead of eviction when maxUnavailable set to 100%, which is important for [minimum downtime strategy](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#minimum-downtime-strategy). See [#1706](https://github.com/VictoriaMetrics/operator/issues/1706).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): VMPodScrape for VLAgent and VMAgent now uses the correct port; previously it used the wrong port and could cause scrape failures. See [#1887](https://github.com/VictoriaMetrics/operator/issues/1887).
* BUGFIX: [vmdistributed](https://docs.victoriametrics.com/operator/resources/vmdistributed/): fix PVC being owned by StatefulSet and top-level object simultaenously. See [#1845](https://github.com/VictoriaMetrics/operator/issues/1845).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): remove unneeded finalizer from core K8s resources. See [#835](https://github.com/VictoriaMetrics/operator/issues/835).
* BUGFIX: [vmdistributed](https://docs.victoriametrics.com/operator/resources/vmdistributed/): remove finalizers from VMServiceScrape and VMPodScrape objects, and keep finalizers on VMAgent, VMCluster, and VMAuth when DeletionTimestamp is not empty.
* BUGFIX: [vmsingle](https://docs.victoriametrics.com/operator/resources/vmsingle/) and [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): previously, ingest-only mode could still mount scrape configuration secrets when relabeling or stream aggregation was configured, which caused unexpected secret mounts and RBAC-related failures; now these secrets are not mounted in ingest-only mode, so deployments start with the expected minimal permissions and avoid related runtime errors. See [#1828](https://github.com/VictoriaMetrics/operator/issues/1828).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): do not recreate STS if VCT size was increased and recreate in other cases.
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): wait for STS deletion in case of recreation without throwing an error.

## [v0.68.1](https://github.com/VictoriaMetrics/operator/releases/tag/v0.68.1)
**Release date:** 23 Feb 2026

* SECURITY: upgrade Go builder from Go1.25.5 to Go1.25.7. See [the list of issues addressed in Go1.25.7](https://github.com/golang/go/issues?q=milestone%3AGo1.25.7+label%3ACherryPickApproved).

* BUGFIX: [vmanomaly](https://docs.victoriametrics.com/operator/resources/vmanomaly/): fix configuration marshalling for [Prophet model](https://docs.victoriametrics.com/anomaly-detection/components/models/#prophet). Previously, using Prophet model would lead to panic during configuration marshalling.

## [v0.68.0](https://github.com/VictoriaMetrics/operator/releases/tag/v0.68.0)
**Release date:** 23 Feb 2026

**Update note 1**: deprecated VMProbe's `spec.targets.ingress`. Use `spec.targets.kubernetes` slice instead. Please check [example of VMProbe with Ingress discovery](https://github.com/VictoriaMetrics/operator/blob/master/config/examples/vmprobe-k8s.yaml). This field will be removed in v0.71.0.

**Update note 2**: deprecated VMProbe's `spec.targets.staticConfig`. Use `spec.targets.static` instead. Please check [example of VMProbe with static targets](https://github.com/VictoriaMetrics/operator/blob/master/config/examples/vmprobe.yaml). This field will be removed in v0.71.0.

* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VM apps to [v1.136.0](https://github.com/VictoriaMetrics/VictoriaMetrics/releases/tag/v1.136.0) version
* Dependency: [vmoperator](https://docs.victoriametrics.com/operator/): Updated default versions for VL apps to [v1.45.0](https://github.com/VictoriaMetrics/VictoriaLogs/releases/tag/v1.45.0).

* FEATURE: [vmalertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager/): added namespace to `--cluster.peer` arguments explicitly when `spec.clusterDomainName` is omitted and added unit tests to test this.
* FEATURE: [vmoperator](https://docs.victoriametrics.com/operator/): introduce `VMDistributed` CR, which helps to propagate changes to each zone without affecting global availability. Before distributed setup deployment was multistep manual action. See [#1515](https://github.com/VictoriaMetrics/operator/issues/1515).
* FEATURE: [vlagent](https://docs.victoriametrics.com/operator/resources/vlagent/): support ability to override default stream fields for vlagent in logs collection mode.
* FEATURE: [vmoperator](https://docs.victoriametrics.com/operator/): added `VM_*_EPHEMERAL_STORAGE_REQUEST` and `VM_*_EPHEMERAL_STORAGE_LIMIT` global variables that allow to configure ephemeralStorage requests and limits. See [#1711](https://github.com/VictoriaMetrics/operator/issues/1711).
* FEATURE: [vmalertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager/): added tracing support. See [#1770](https://github.com/VictoriaMetrics/operator/issues/1770).
* FEATURE: [vmprobe](https://docs.victoriametrics.com/operator/resources/vmprobe/): added `spec.targets.kubernetes` property, that allows to configure probe for `ingress`, `pod` and `service` roles. See [#1078](https://github.com/VictoriaMetrics/operator/issues/1078) and [#1716](https://github.com/VictoriaMetrics/operator/issues/1716).
* FEATURE: [vmscrapeconfig](https://docs.victoriametrics.com/operator/resources/vmscrapeconfig/): added nomad_sd_config support. See [#1809](https://github.com/VictoriaMetrics/operator/issues/1809).
* FEATURE: [vmoperator](https://docs.victoriametrics.com/operator/): support VPA for vmcluster, vtcluster, vlcluster and vmauth. See [#1795](https://github.com/VictoriaMetrics/operator/issues/1795). Thanks to the @dctrwatson for the pull request [#1803](https://github.com/VictoriaMetrics/operator/pull/1803).

* BUGFIX: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): previously the operator requested `nodes/proxy` RBAC permissions even though vmagent did not use them; now this permission is no longer required, reducing the default privilege footprint for users running vmagent. See [#1753](https://github.com/VictoriaMetrics/operator/issues/1753).
* BUGFIX: [vmalert](https://docs.victoriametrics.com/operator/resources/vmalert/): throw error if no notifiers found. See [#1757](https://github.com/VictoriaMetrics/operator/issues/1757).
* BUGFIX: [vlagent](https://docs.victoriametrics.com/operator/resources/vlagent/): previously the operator emitted quoted `spec.k8sCollector.{msgField,timeField,ignoreFields,decolorizeFields}` values, which caused vlagent to misparse these fields; now these fields are emitted unquoted so collector settings are applied correctly. See [#1749](https://github.com/VictoriaMetrics/operator/issues/1749).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): fixed conflicts for `VMAlert`, `VMAlertmanager` and `VMAuth` reconcilers, which are updating same objects concurrently with reconcilers for their child objects.
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): previously PVC downscaling always emitted a warning, which is not expected, while using PVC autoresizer; now warning during attempt to downsize PVC is only emitted if `operator.victoriametrics.com/pvc-allow-volume-expansion: false` is not set. See [#1747](https://github.com/VictoriaMetrics/operator/issues/1747).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): skip self scrape objects management if respective controller is disabled. See [#1718](https://github.com/VictoriaMetrics/operator/issues/1718).
* BUGFIX: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): support both prometheus-compatible `endpointslice` and old `endpointslices` roles.
* BUGFIX: [vmanomaly](https://docs.victoriametrics.com/operator/resources/vmanomaly/): fix pod metrics port in the default VMPodScrape.
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): support Prometheus operator AlertmanagerConfig spec.muteTimeIntervals conversion to VMAlertmanagerConfig spec.timeIntervals. See [#1783](https://github.com/VictoriaMetrics/operator/issues/1783).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): previously StatefulSet/Deployment/DaemonSet rollouts could proceed in parallel, now pods are rolled out sequentially. See [#1693](https://github.com/VictoriaMetrics/operator/issues/1693).
* BUGFIX: [config-reloader](https://github.com/VictoriaMetrics/operator/tree/master/cmd/config-reloader): previously `--only-init-config` still kept the reloader running background watchers; now it exits after the initial config update so the pod terminates as expected. See [#1785](https://github.com/VictoriaMetrics/operator/issues/1785).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): previously, recreating a resource after deletion could hang and block updates; now resource recreation completes normally. See [#1707](https://github.com/VictoriaMetrics/operator/issues/1707).
* BUGFIX: [vmoperator](https://docs.victoriametrics.com/operator/): use global image registry unless image.repository is defined. See [#1813](https://github.com/VictoriaMetrics/operator/issues/1813).
* BUGFIX: [vmalertmanagerconfig](https://docs.victoriametrics.com/operator/resources/vmalertmanagerconfig/): previously spec.route and spec.receivers were required; now both parameters are optional to align with prometheus operator. VMAlertmanager now can be used to set just the global inhibition rules. See [#1800](https://github.com/VictoriaMetrics/operator/issues/1800).
* BUGFIX: [vmagent](https://docs.victoriametrics.com/operator/resources/vmagent/): fixed RBAC, when ingestOnlyMode is enabled and relabel of stream aggregation configurations defined. See [#1828](https://github.com/VictoriaMetrics/operator/issues/1828).
