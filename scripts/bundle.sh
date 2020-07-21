#!/usr/bin/env sh
set -e
set -x


mkdir -p install/crds/vm
mkdir install/operator
mkdir install/examples
cp config/crds/victoriametrics* install/crds/vm/
cp deploy/*.yaml install/operator/
cp deploy/examples/* install/examples/
if [ $TAG  ];then
  sed -i -e "s/:latest/:$TAG/" install/operator/operator.yaml
fi

zip -r operator.zip vm-operator
zip -r bundle_crd.zip install/
rm -rf install/