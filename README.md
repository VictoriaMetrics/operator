# VictoriaMetrics operator


## Documentation

- quick start [doc](/docs/quick-start.MD)
- high availability [doc](/docs/high-availability.MD)
- relabeling configuration [doc](/docs/relabeling.MD)
- managing crd objects versions [doc](/docs/managing-versions.MD)
- design and description of implementation [design](/docs/design.MD)
- operator objects description [doc](/docs/api.MD)





## kubernetes compatibility versions

operator tested at kubernetes versions 
from 1.13 to 1.18

## development

- operator-sdk verson v1.0.0 +  [https://github.com/operator-framework/operator-sdk]
- golang 1.15 +
- minikube or kind

start:
```bash
make run
```

for test execution run:
```bash
#unit tests
make test 

# you need minikube for e2e, do not run it on live cluster
#e2e tests with local binary
make e2e-local
```
