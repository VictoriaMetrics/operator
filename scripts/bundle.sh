#!/usr/bin/env sh
set -e
set -x


mkdir -p install/crds/
mkdir install/operator
mkdir install/examples
kustomize build config/crd > install/crds/crd.yaml
kustomize build config/rbac > install/operator/rbac.yaml
cp config/examples/*.yaml install/examples/

if [ $TAG ];then
    cd config/default
    kustomize edit  set image manager=$TAG
    cd -
fi

kustomize build config/manager > install/operator/manager.yaml

zip -r operator.zip bin/manager
zip -r bundle_crd.zip install/
rm -rf install/