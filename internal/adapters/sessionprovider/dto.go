package sessionprovider

type scaleOutRequest struct {
	TaskID       string `json:"taskId"`
	SessionCount int    `json:"sessionCount"`
}
