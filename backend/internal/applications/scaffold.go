package applications

import (
	"fmt"
	"os"
	"path/filepath"
)

// An empty source gets a small editable starter, only while its directory is empty.
func scaffoldEmptySource(root string, config map[string]any) error {
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return err
	}
	files := map[string]string{}
	switch configString(config, "runtime") {
	case "php":
		files["index.php"] = "<?php echo 'DevBox PHP · ' . PHP_VERSION;\n"
	case "python":
		files["requirements.txt"] = "fastapi\nuvicorn\n"
		files["main.py"] = "from fastapi import FastAPI\napp = FastAPI()\n@app.get('/')\ndef index():\n    return {'message': 'DevBox Python'}\n"
	case "go":
		files["go.mod"] = "module devbox.local/app\n\ngo 1.23\n"
		files["main.go"] = "package main\nimport (\"net/http\"; \"os\")\nfunc main() { port := os.Getenv(\"PORT\"); if port == \"\" { port = \"8080\" }; http.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(\"DevBox Go\")) }); http.ListenAndServe(\":\"+port, nil) }\n"
	case "node":
		files["package.json"] = "{\"private\":true,\"scripts\":{\"start\":\"node server.js\"}}\n"
		files["server.js"] = "require('http').createServer((req,res)=>res.end('DevBox Node.js')).listen(process.env.PORT || 8080,'0.0.0.0');\n"
	case "static", "":
		files["index.html"] = "<!doctype html><meta charset=\"utf-8\"><title>DevBox</title><h1>DevBox</h1>\n"
	default:
		return fmt.Errorf("%w: choose a technology for an empty application", ErrInvalidInput)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			return err
		}
	}
	return nil
}
