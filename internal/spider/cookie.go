package spider

import (
	"fmt"
	"os"

	"github.com/robertkrimen/otto"
)

func GenerateTonghuashunVCookie(vJSPath string) (string, error) {
	bytes, err := os.ReadFile(vJSPath)
	if err != nil {
		return "", fmt.Errorf("read v js file %s failed: %w", vJSPath, err)
	}

	vm := otto.New()
	if _, err := vm.Run(string(bytes)); err != nil {
		return "", fmt.Errorf("run v js failed: %w", err)
	}

	value, err := vm.Call("v", nil)
	if err != nil {
		return "", fmt.Errorf("call v() failed: %w", err)
	}

	v, err := value.ToString()
	if err != nil {
		return "", fmt.Errorf("convert v cookie failed: %w", err)
	}
	if v == "" {
		return "", fmt.Errorf("generated empty v cookie")
	}
	return v, nil
}
