# VMUser

The `VMUser` CRD describes user configuration, its authentication methods `basic auth` or `Authorization` header. 
User access permissions, with possible routing information.

User can define routing target with `static` config, by entering target `url`, or with `CRDRef`, in this case, 
operator queries kubernetes API, retrieves information about CRD and builds proper url.

## Authentication methods

### Basic auth

Basic auth is the simplest way to authenticate user. User defines `username` and `password` fields in `auth` section.

### Bearer token

Bearer token is a way to authenticate user with `Authorization` header. User defines `token` field in `auth` section.

## Routing

User can define routing target with `static` config, by entering target `url`, or with `CRDRef`, in this case,
operator queries kubernetes API, retrieves information about CRD and builds proper url.

### Static

<!-- TODO -->

### CRDRef

<!-- TODO -->

## Specification

You can see the full actual specification of the `VMUser` resource in
the [API docs -> VMUser](https://docs.victoriametrics.com/operator/api.html#vmuser).
