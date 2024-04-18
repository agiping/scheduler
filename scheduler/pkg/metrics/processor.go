package metrics

type Processor interface {
	Process() error
}
