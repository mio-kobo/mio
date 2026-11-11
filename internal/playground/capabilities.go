package playground

import (
	"fmt"

	"Mio/mma"
)

// Mochi files declare the native functions they may call as stubs, and the reference VM
// binds every stub before the first instruction. The pure-Mio native CLI always declares
// the kiso/fs runtime stubs, even for programs that never touch a file.
//
// The browser has no host file system or sockets, so each host function of the reference
// CLI (internal/nativefs, internal/nativesocket) is registered with its exact signature but
// an implementation that only fails. A program that never calls one runs normally; a call
// stops the VM with a runtime error. No host capability is reachable. The signatures are
// copied rather than imported because those packages do not build for js/wasm;
// TestDeniedRegistryMatchesReference keeps the copy identical to the reference registry.

type hostFunction struct {
	name   string
	params []mma.Type
	result mma.Type
}

var (
	r, b, s, i = mma.TypeResource, mma.TypeBytes, mma.TypeString, mma.TypeInt64
	// HostModules lists every native MMA function of the reference CLI.
	hostModules = map[string][]hostFunction{
		"mio/fs/runtime": {
			{"open", []mma.Type{s, i}, r}, {"file_status", []mma.Type{r, b}, s},
			{"read", []mma.Type{r, b, i, i, b}, s}, {"write", []mma.Type{r, b, i, i, b}, s},
			{"close", []mma.Type{r, b}, s}, {"stat", []mma.Type{s, b}, s}, {"mkdir", []mma.Type{s, b}, s},
			{"open_dir", []mma.Type{s}, r}, {"dir_status", []mma.Type{r, b}, s},
			{"dir_next", []mma.Type{r}, mma.TypeBool}, {"dir_name", []mma.Type{r}, s},
			{"dir_is_dir", []mma.Type{r}, mma.TypeBool}, {"dir_is_file", []mma.Type{r}, mma.TypeBool},
			{"dir_close", []mma.Type{r, b}, s},
		},
		"mio/sys/socket": {
			{"open", nil, r}, {"status", []mma.Type{r, b}, s},
			{"bind_ipv4", []mma.Type{r, b, i, b}, s}, {"connect_ipv4", []mma.Type{r, b, i, b}, s},
			{"listen", []mma.Type{r, i, b}, s}, {"accept", []mma.Type{r}, r},
			{"read", []mma.Type{r, b, i, i, b}, s}, {"write", []mma.Type{r, b, i, i, b}, s},
			{"close", []mma.Type{r, b}, s},
			{"set_nonblocking", []mma.Type{r, i, b}, s}, {"finish_connect", []mma.Type{r, b}, s},
			{"reactor_open", []mma.Type{i}, r}, {"reactor_ctl", []mma.Type{r, r, i, b}, s}, {"reactor_wait", []mma.Type{r, b, i, i, b}, s},
			{"reactor_signal", []mma.Type{r, b}, s}, {"reactor_ack", []mma.Type{r, b}, s},
		},
	}
)

// ErrCapability is wrapped by every denied host call.
var ErrCapability = fmt.Errorf("host capability is not available in the Web playground")

// DeniedRegistry returns a fresh registry in which every host function is denied.
func DeniedRegistry() (*mma.ModuleRegistry, error) {
	denied := mma.NewModuleRegistry()
	for path, functions := range hostModules {
		definitions := make([]mma.FunctionDefinition, 0, len(functions))
		for _, function := range functions {
			qualified := path + "." + function.name
			definitions = append(definitions, mma.FunctionDefinition{
				Name: function.name, Public: true,
				Signature: mma.FunctionSignature{Parameters: function.params, Return: function.result},
				Function:  func(*mma.CallContext) error { return fmt.Errorf("%w: %s", ErrCapability, qualified) },
			})
		}
		if err := denied.RegisterModule(mma.ModuleDefinition{Path: path, Functions: definitions}); err != nil {
			return nil, err
		}
	}
	return denied, nil
}
