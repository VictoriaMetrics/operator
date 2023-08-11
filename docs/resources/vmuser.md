# VMUser

The `VMUser` CRD describes user configuration, its authentication methods `basic auth` or `Authorization` header. 
User access permissions, with possible routing information.

User can define routing target with `static` config, by entering target `url`, or with `CRDRef`, in this case, 
operator queries kubernetes API, retrieves information about CRD and builds proper url.

## Specification

You can see the full actual specification of the `VMUser` resource in
the [API docs -> VMUser](https://docs.victoriametrics.com/vmoperator/api.html#vmuser).

**TODO**

