package domain

// RemoteCamera holds information about a camera exposed by a remote accelerator.
type RemoteCamera struct {
	SensorID    int32
	DisplayName string
	Model       string
}
