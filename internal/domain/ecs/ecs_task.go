package ecs

import "legacy-messenger-control-plane/internal/domain/task"

type ECSTask struct {
	TaskID        string
	TaskARN       string
	LastStatus    string
	DesiredStatus string

	// EC2 인스턴스 Private IP
	PrivateIP string

	//bridge 모드에서 managementPort에 대응되는 실제 host port
	HostPort int

	// bridge 모드에서 필요한 정보 추가
	ContainerInstanceARN string

	NetworkBindings []task.NetworkBindingInfo
}
