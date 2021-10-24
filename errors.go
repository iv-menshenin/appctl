package appctl

type appError string

const (
	ErrWrongState  appError = "wrong application state"
	ErrMainOmitted appError = "main function is omitted"
	ErrShutdown    appError = "application is in shutdown state"
)

func (e appError) Error() string {
	return string(e)
}
