package translator

// Translator converts a provider-specific upstream error body into an
// OpenAI-compatible error body. Returning nil means the caller should use the
// raw body unchanged.
type Translator interface {
	Translate(statusCode int, rawBody []byte) []byte
}

// Func adapts a plain function to the Translator interface.
type Func func(int, []byte) []byte

func (f Func) Translate(statusCode int, rawBody []byte) []byte {
	return f(statusCode, rawBody)
}

var registry = map[string]Translator{}

// Register binds a translator to a provider prefix (e.g. "cf", "claude").
func Register(providerPrefix string, t Translator) {
	if t == nil {
		delete(registry, providerPrefix)
		return
	}
	registry[providerPrefix] = t
}

// Translate runs the translator registered for providerPrefix, if any.
func Translate(providerPrefix string, statusCode int, rawBody []byte) []byte {
	if t, ok := registry[providerPrefix]; ok {
		return t.Translate(statusCode, rawBody)
	}
	return nil
}

// Has reports whether a translator exists for the provider prefix.
func Has(providerPrefix string) bool {
	_, ok := registry[providerPrefix]
	return ok
}
