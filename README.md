# VictoriaMetrics operator

## Documentation

- quick start [doc](/docs/quick-start.MD)
- design and description of implementation [design](/docs/design.MD)
- high availability [doc](/docs/high-availability.MD)
- operator objects description [doc](/docs/api.MD)
- backup [doc](/docs/backup.MD)





## limitations

- alert relabel is not supported

## development

- operator-sdk verson v0.17.0 +  [https://github.com/operator-framework/operator-sdk]
- golang 1.13 +
- minikube 

start:
```bash
make run
```