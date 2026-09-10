//go:build cgo && libjanet

package main

/*
#cgo pkg-config: janet
#include <janet.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

static char *janetpls_copy_bytes(const uint8_t *bytes, int32_t length) {
    char *copy = malloc((size_t) length + 1);
    if (copy == NULL) return NULL;
    memcpy(copy, bytes, (size_t) length);
    copy[length] = '\0';
    return copy;
}

static char *janetpls_copy_janet_string(JanetString string) {
    return janetpls_copy_bytes(string, janet_string_length(string));
}

static const char janetpls_flycheck_wrapper[] =
    "(defn janetpls/check [file-path source-path]\n"
    "  (def output @\"\")\n"
    "  (with-dyns [*err* output]\n"
    "    (flycheck file-path :source source-path))\n"
    "  (string output))\n";

static int janetpls_flycheck(
    const char *file_path,
    const char *source_path,
    char **output
) {
    if (janet_init()) {
        static const char initialization_error[] = "libjanet initialization failed";
        *output = janetpls_copy_bytes(
            (const uint8_t *) initialization_error,
            sizeof(initialization_error) - 1
        );
        return -1;
    }

    JanetTable *environment = janet_core_env(NULL);
    janet_gcroot(janet_wrap_table(environment));

    Janet bootstrap_result;
    int bootstrap_status = janet_dostring(
        environment,
        janetpls_flycheck_wrapper,
        "janetpls/flycheck.janet",
        &bootstrap_result
    );
    if (bootstrap_status) {
        janet_gcroot(bootstrap_result);
        *output = janetpls_copy_janet_string(janet_to_string(bootstrap_result));
        janet_gcunroot(bootstrap_result);
        janet_gcunroot(janet_wrap_table(environment));
        janet_deinit();
        return -1;
    }

    Janet check;
    janet_resolve(environment, janet_csymbol("janetpls/check"), &check);
    if (!janet_checktype(check, JANET_FUNCTION)) {
        static const char resolution_error[] = "libjanet flycheck function is unavailable";
        *output = janetpls_copy_bytes(
            (const uint8_t *) resolution_error,
            sizeof(resolution_error) - 1
        );
        janet_gcunroot(janet_wrap_table(environment));
        janet_deinit();
        return -1;
    }

    Janet arguments[2];
    arguments[0] = janet_cstringv(file_path);
    janet_gcroot(arguments[0]);
    arguments[1] = janet_cstringv(source_path);
    janet_gcroot(arguments[1]);

    Janet result;
    JanetFiber *fiber = NULL;
    JanetSignal signal = janet_pcall(
        janet_unwrap_function(check),
        2,
        arguments,
        &result,
        &fiber
    );
    if (signal != JANET_SIGNAL_OK) {
        janet_gcroot(result);
        *output = janetpls_copy_janet_string(janet_to_string(result));
        janet_gcunroot(result);
        janet_gcunroot(arguments[1]);
        janet_gcunroot(arguments[0]);
        janet_gcunroot(janet_wrap_table(environment));
        janet_deinit();
        return -1;
    }

    if (!janet_checktype(result, JANET_STRING)) {
        static const char result_error[] = "libjanet flycheck returned a non-string result";
        *output = janetpls_copy_bytes(
            (const uint8_t *) result_error,
            sizeof(result_error) - 1
        );
        janet_gcunroot(arguments[1]);
        janet_gcunroot(arguments[0]);
        janet_gcunroot(janet_wrap_table(environment));
        janet_deinit();
        return -1;
    }

    *output = janetpls_copy_janet_string(janet_unwrap_string(result));
    janet_gcunroot(arguments[1]);
    janet_gcunroot(arguments[0]);
    janet_gcunroot(janet_wrap_table(environment));
    janet_deinit();
    return *output == NULL ? -1 : 0;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"github.com/jhaton/janetpls/internal/compiler"
)

func compileSource(request compiler.Request) (compiler.Response, error) {
	sourceFile, err := os.CreateTemp("", "janetpls-compiler-*.janet")
	if err != nil {
		return compiler.Response{}, fmt.Errorf("create flycheck source: %w", err)
	}
	sourcePath := sourceFile.Name()
	defer os.Remove(sourcePath)
	if _, err := sourceFile.WriteString(request.Source); err != nil {
		sourceFile.Close()
		return compiler.Response{}, fmt.Errorf("write flycheck source: %w", err)
	}
	if err := sourceFile.Close(); err != nil {
		return compiler.Response{}, fmt.Errorf("close flycheck source: %w", err)
	}

	filePath := C.CString(sourcePath)
	defer C.free(unsafe.Pointer(filePath))
	displayPath := C.CString(request.Path)
	defer C.free(unsafe.Pointer(displayPath))
	var output *C.char
	status := C.janetpls_flycheck(filePath, displayPath, &output)
	if output != nil {
		defer C.free(unsafe.Pointer(output))
	}
	if status < 0 {
		if output == nil {
			return compiler.Response{}, errors.New("libjanet flycheck failed")
		}
		return compiler.Response{}, errors.New(C.GoString(output))
	}
	if output == nil {
		return compiler.Response{}, errors.New("libjanet flycheck returned no output")
	}
	return compiler.Response{
		Diagnostics: parseFlycheckDiagnostics(C.GoString(output), request.Path),
	}, nil
}

func parseFlycheckDiagnostics(output, path string) []compiler.Diagnostic {
	diagnostics := []compiler.Diagnostic{}
	prefix := "error: " + path + ":"
	for line := range strings.SplitSeq(output, "\n") {
		location, found := strings.CutPrefix(line, prefix)
		if !found {
			continue
		}
		lineText, location, found := strings.Cut(location, ":")
		if !found {
			continue
		}
		columnText, message, found := strings.Cut(location, ":")
		if !found {
			continue
		}
		lineNumber, lineError := strconv.Atoi(lineText)
		columnNumber, columnError := strconv.Atoi(columnText)
		if lineError != nil || columnError != nil {
			continue
		}
		message = strings.TrimSpace(message)
		for _, kind := range [...]string{"compile error: ", "parse error: ", "runtime error: "} {
			if trimmed, ok := strings.CutPrefix(message, kind); ok {
				message = trimmed
				break
			}
		}
		diagnostics = append(diagnostics, compiler.Diagnostic{
			Line:    max(lineNumber-1, 0),
			Column:  max(columnNumber-1, 0),
			Message: message,
		})
	}
	return diagnostics
}
