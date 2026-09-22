package remote

type devicePower interface {
	PowerOn() error
}

type acceleratorHealth interface {
	IsAvailable() bool
}
