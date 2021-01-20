#!/bin/bash

set -e

echo -n "Username: "
read USERNAME
echo -n "Password: "
read -s PASSWORD
echo

curl -H "Content-Type: application/json" -XPOST https://quay.io/cnr/api/v1/users/login -d '
{
    "user": {
        "username": "'"${USERNAME}"'",
        "password": "'"${PASSWORD}"'"
    }
}' | jq -r .token