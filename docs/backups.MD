## VMBackuper


 `VMSingle` and `VMCluster` has built-in backup configuration, it uses `vmbackuper` - tools for logical backups.
 It supports incremental backups (hours, daily, etc) with popular object storages (aws s3, google cloud storage).
 
  You can enable it with the simple configuration, define secret
  
  ```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: remote-storage-keys
type: Opaque
stringData:
  credentials: |-
    [default]
    AWS_ACCESS_KEY_ID = your_access_key_id
    AWS_SECRET_ACCESS_KEY = your_secret_access_key
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMSingle
metadata:
  name: example-vmsingle
spec:
  # Add fields here
  retentionPeriod: "1"
  vmBackup:
    destintaion: "s3://bucket/path/to/backup/dir"
    credentialsSecret:
      name: remote-storage-keys
      key: credentials
---
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: example-vmcluster-persistent
spec:
  retentionPeriod: "4"
  replicationFactor: 2
  vmstorage:
    replicaCount: 2
    vmBackup:
      destintaion: "s3://bucket/path/to/backup/dir"
      credentialsSecret:
        name: remote-storage-keys
        key: credentials

```


 You can read more about backup configuration options and mechanics [here](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmbackup)
 
 Possible configuration options for backup crd can be found at [link](/docs/api.MD#vmbackup)
 
 
## Restoring backups


 It can be done with [vmrestore](https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmrestore)
 
 You have to stop `VMSingle` by scaling it replicas to zero and manually restore data to the database directory.
 
 Steps:
 1) edit `VMSingle` CRD, set replicaCount: 0
 2) wait until database stops
 3) ssh to some server, where you can mount `VMSingle` disk and mount it manually
 4) restore files with `vmrestore`
 5) umount disk
 6) edit `VMSingle` CRD, set replicaCount: 1
 7) wait database start