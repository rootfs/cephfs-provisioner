package main

import (
	"errors"
	"flag"
	"os"
	"path"
	"time"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/nfs-provisioner/controller"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/types"
	"k8s.io/client-go/pkg/util/uuid"
	"k8s.io/client-go/pkg/util/wait"
	"k8s.io/client-go/rest"
)

const (
	resyncPeriod              = 15 * time.Second
	provisionerName           = "kubernetes.io/cephfs"
	exponentialBackOffOnError = false
	failedRetryThreshold      = 5
)

type cephFSProvisioner struct {
	// Required: Monitors is a collection of Ceph monitors
	Monitors []string `json:"monitors"`
	// Optional: User is the rados user name, default is admin
	Admin string `json:"admin,omitempty"`
	// Required: Secret is Admin's secret.
	Secret string `json:"secret,omitempty"`

	// Identity of this cephFSProvisioner, generated. Used to identify "this"
	// provisioner's PVs.
	identity types.UID
}

func NewCephFSProvisioner() controller.Provisioner {
	return &cephFSProvisioner{
		identity: uuid.NewUUID(),
	}
}

var _ controller.Provisioner = &cephFSProvisioner{}

// Provision creates a storage asset and returns a PV object representing it.
func (p *cephFSProvisioner) Provision(options controller.VolumeOptions) (*v1.PersistentVolume, error) {
	//TODO: call out cephfs-provisioner
	pv := &v1.PersistentVolume{
		ObjectMeta: v1.ObjectMeta{
			Name: options.PVName,
			Annotations: map[string]string{
				"cephFSProvisionerIdentity": string(p.identity),
			},
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: options.PersistentVolumeReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			Capacity: v1.ResourceList{ //FIXME: kernel cephfs doesn't enforce quota, capacity is not meaningless here.
				v1.ResourceName(v1.ResourceStorage): options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				CephFS: &v1.CephFSVolumeSource{
					Path: path,
				},
			},
		},
	}

	return pv, nil
}

// Delete removes the storage asset that was created by Provision represented
// by the given PV.
func (p *cephFSProvisioner) Delete(volume *v1.PersistentVolume) error {
	ann, ok := volume.Annotations["CephFSProvisionerIdentity"]
	if !ok {
		return errors.New("identity annotation not found on PV")
	}
	if ann != string(p.identity) {
		return &controller.IgnoredError{"identity annotation on PV does not match ours"}
	}

	path := path.Join(p.pvDir, volume.Name)
	if err := os.RemoveAll(path); err != nil {
		return err
	}

	return nil
}

func main() {
	flag.Parse()
	flag.Set("logtostderr", "true")

	// Create an InClusterConfig and use it to create a client for the controller
	// to use to communicate with Kubernetes
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatalf("Failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}

	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		glog.Fatalf("Error getting server version: %v", err)
	}

	// Create the provisioner: it implements the Provisioner interface expected by
	// the controller
	cephFSProvisioner := NewCephFSProvisioner()

	// Start the provision controller which will dynamically provision cephFS
	// PVs
	pc := controller.NewProvisionController(clientset, resyncPeriod, provisionerName, cephFSProvisioner, serverVersion.GitVersion, exponentialBackOffOnError, failedRetryThreshold)
	pc.Run(wait.NeverStop)
}
