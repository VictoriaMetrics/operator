# VMAlertmanagerConfig

The `VMAlertmanagerConfig` provides way to configure [VMAlertmanager](https://docs.victoriametrics.com/operator/resources/vmalertmanager.html) 
configuration with CRD. It allows to define different configuration parts, which will be merged by operator into config. 

It behaves like other config parts - `VMServiceScrape` and etc.

## Specification

You can see the full actual specification of the `VMAlertmanagerConfig` resource in 
the **[API docs -> VMAlertmanagerConfig](https://docs.victoriametrics.com/operator/api.html#vmalertmanagerconfig)**.

## Using

`VMAlertmanagerConfig` allows delegating notification configuration to the kubernetes cluster users.
The application owner may configure notifications by defining it at `VMAlertmanagerConfig`.

With the combination of `VMRule` and `VMServiceScrape` it allows delegating configuration observability to application owners, and uses popular `GitOps` practice.

Operator combines `VMAlertmanagerConfig`s into a single configuration file for `VMAlertmanager`.

```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMAlertmanagerConfig
metadata:
  name: example-email-web
  namespace: production
spec:
  route:
    receiver: email
    group_interval: 1m
    routes:
      - receiver: email
        matchers:
          - {severity =~ "warning|critical", app_name = "blog"}
  receivers:
    - name: email
      email_configs:
        - to: some-email@example.com
          from: alerting@example.com
          smarthost: example.com:25
          text: ALARM
```

#### Special Case

VMAlertmanagerConfig has enforced namespace matcher.
Alerts must have a proper namespace label, with the same value as name of namespace for VMAlertmanagerConfig.

It can be disabled, by setting the following value to the VMAlertmanager: `spec.disableNamespaceMatcher: true`.
