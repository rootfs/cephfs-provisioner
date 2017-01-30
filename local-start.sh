PATH=${PATH}:`pwd`
cephfs-provisioner -master=http://127.0.0.1:8080 -kubeconfig=/root/.kube/config -logtostderr

