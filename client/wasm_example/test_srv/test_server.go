package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os/exec"
	"time"
)

const htmlTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>NetBird Test Server</title>
    <style>
        :root {
            --nb-orange: #f68330;
            --nb-gray-950: #181a1d;
            --nb-gray-900: #32363d;
            --nb-gray-800: #3f444b;
            --nb-gray-100: #e4e7e9;
            --nb-blue: #31e4f5;
        }
        
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: linear-gradient(135deg, var(--nb-gray-950) 0%, var(--nb-gray-900) 100%);
            color: var(--nb-gray-100);
            min-height: 100vh;
            padding: 20px;
        }
        
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        
        .header {
            text-align: center;
            padding: 40px 0;
            border-bottom: 1px solid var(--nb-gray-800);
            margin-bottom: 40px;
        }
        
        h1 {
            font-size: 2.5em;
            background: linear-gradient(90deg, var(--nb-orange), var(--nb-blue));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 10px;
        }
        
        .subtitle {
            color: #aab4bd;
            font-size: 1.1em;
        }
        
        .grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 20px;
            margin-bottom: 40px;
        }
        
        .card {
            background: var(--nb-gray-900);
            border: 1px solid var(--nb-gray-800);
            border-radius: 8px;
            padding: 20px;
            transition: all 0.3s ease;
        }
        
        .card:hover {
            transform: translateY(-2px);
            box-shadow: 0 10px 30px rgba(0,0,0,0.3);
            border-color: var(--nb-orange);
        }
        
        .card h2 {
            color: var(--nb-orange);
            margin-bottom: 15px;
            font-size: 1.3em;
        }
        
        .nav {
            background: var(--nb-gray-900);
            border-radius: 8px;
            padding: 15px;
            margin-bottom: 30px;
        }
        
        .nav ul {
            list-style: none;
            display: flex;
            gap: 20px;
            flex-wrap: wrap;
            justify-content: center;
        }
        
        .nav a {
            color: var(--nb-gray-100);
            text-decoration: none;
            padding: 8px 16px;
            border-radius: 4px;
            transition: all 0.3s ease;
            display: inline-block;
        }
        
        .nav a:hover {
            background: var(--nb-orange);
            color: white;
        }
        
        .btn {
            background: var(--nb-orange);
            color: white;
            border: none;
            padding: 12px 24px;
            border-radius: 6px;
            cursor: pointer;
            font-size: 1em;
            transition: all 0.3s ease;
            display: inline-block;
            margin: 10px 5px;
        }
        
        .btn:hover {
            background: #e55311;
            transform: translateY(-1px);
            box-shadow: 0 5px 15px rgba(246, 131, 48, 0.3);
        }
        
        .btn:active {
            transform: translateY(0);
        }
        
        .btn.secondary {
            background: var(--nb-blue);
        }
        
        .btn.secondary:hover {
            background: #00c4da;
            box-shadow: 0 5px 15px rgba(49, 228, 245, 0.3);
        }
        
        .status-output {
            background: #1a1d21;
            border: 1px solid var(--nb-gray-800);
            border-radius: 6px;
            padding: 20px;
            margin-top: 20px;
            font-family: 'Courier New', monospace;
            white-space: pre-wrap;
            word-wrap: break-word;
            max-height: 500px;
            overflow-y: auto;
            display: none;
        }
        
        .status-output.show {
            display: block;
        }
        
        .loading {
            display: none;
            color: var(--nb-blue);
            margin-top: 10px;
        }
        
        .loading.show {
            display: block;
        }
        
        .spinner {
            display: inline-block;
            width: 20px;
            height: 20px;
            border: 3px solid rgba(49, 228, 245, 0.3);
            border-top-color: var(--nb-blue);
            border-radius: 50%;
            animation: spin 1s linear infinite;
            margin-right: 10px;
            vertical-align: middle;
        }
        
        @keyframes spin {
            to { transform: rotate(360deg); }
        }
        
        .interactive-section {
            background: var(--nb-gray-900);
            border: 1px solid var(--nb-gray-800);
            border-radius: 8px;
            padding: 30px;
            margin-bottom: 30px;
        }
        
        input[type="text"], textarea {
            background: var(--nb-gray-950);
            border: 1px solid var(--nb-gray-800);
            color: var(--nb-gray-100);
            padding: 10px;
            border-radius: 4px;
            width: 100%;
            margin-bottom: 10px;
        }
        
        input[type="text"]:focus, textarea:focus {
            outline: none;
            border-color: var(--nb-orange);
        }
        
        #messages {
            background: var(--nb-gray-950);
            border: 1px solid var(--nb-gray-800);
            border-radius: 6px;
            padding: 15px;
            margin-top: 20px;
            max-height: 300px;
            overflow-y: auto;
        }
        
        .message {
            padding: 8px;
            margin-bottom: 8px;
            background: var(--nb-gray-900);
            border-radius: 4px;
            border-left: 3px solid var(--nb-orange);
        }
        
        .timestamp {
            color: #7c8994;
            font-size: 0.9em;
            margin-right: 10px;
        }
        
        #canvas {
            border: 2px solid var(--nb-gray-800);
            border-radius: 6px;
            margin-top: 20px;
            cursor: crosshair;
            background: white;
        }
        
        .file-list {
            list-style: none;
            padding: 0;
        }
        
        .file-list li {
            padding: 10px;
            margin-bottom: 5px;
            background: var(--nb-gray-950);
            border-radius: 4px;
            transition: all 0.3s ease;
        }
        
        .file-list li:hover {
            background: var(--nb-gray-800);
            padding-left: 15px;
        }
        
        .file-list a {
            color: var(--nb-blue);
            text-decoration: none;
        }
        
        .error {
            color: #ff6b6b;
            background: rgba(255, 107, 107, 0.1);
            border: 1px solid rgba(255, 107, 107, 0.3);
            padding: 10px;
            border-radius: 4px;
            margin-top: 10px;
        }
        
        .success {
            color: #51cf66;
            background: rgba(81, 207, 102, 0.1);
            border: 1px solid rgba(81, 207, 102, 0.3);
            padding: 10px;
            border-radius: 4px;
            margin-top: 10px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>NetBird Test Server</h1>
            <p class="subtitle">Interactive testing environment for NetBird WASM client</p>
        </div>
        
        <nav class="nav">
            <ul>
                <li><a href="/">Home</a></li>
                <li><a href="/page1">Page 1</a></li>
                <li><a href="/page2">Page 2</a></li>
                <li><a href="/api/data">API Data</a></li>
                <li><a href="/files">Files</a></li>
                <li><a href="test.txt">Test File</a></li>
            </ul>
        </nav>
        
        <div class="interactive-section">
            <h2 style="color: var(--nb-orange); margin-bottom: 20px;">NetBird Status</h2>
            <p style="margin-bottom: 15px;">Click the button below to check NetBird status with detailed information:</p>
            <button class="btn" onclick="getNetBirdStatus()">Get NetBird Status</button>
            <button class="btn secondary" onclick="getNetBirdStatusSimple()">Get Simple Status</button>
            <div class="loading" id="statusLoading">
                <span class="spinner"></span>
                <span>Getting NetBird status...</span>
            </div>
            <div id="statusOutput" class="status-output"></div>
        </div>
        
        <div class="grid">
            <div class="card">
                <h2>JavaScript Interaction</h2>
                <p>Test JavaScript functionality:</p>
                <button class="btn" onclick="showAlert()">Show Alert</button>
                <button class="btn secondary" onclick="updateTime()">Update Time</button>
                <p id="time" style="margin-top: 10px;">Current time will appear here</p>
            </div>
            
            <div class="card">
                <h2>API Test</h2>
                <p>Test API calls:</p>
                <button class="btn" onclick="fetchData()">Fetch Data</button>
                <div id="apiResult" style="margin-top: 10px;"></div>
            </div>
            
            <div class="card">
                <h2>Form Submission</h2>
                <form onsubmit="handleSubmit(event)">
                    <input type="text" id="nameInput" placeholder="Enter your name" required>
                    <button type="submit" class="btn">Submit</button>
                </form>
                <div id="formResult" style="margin-top: 10px;"></div>
            </div>
        </div>
        
        <div class="interactive-section">
            <h2 style="color: var(--nb-orange); margin-bottom: 20px;">Message Board</h2>
            <textarea id="messageInput" placeholder="Type your message here..." rows="3"></textarea>
            <button class="btn" onclick="postMessage()">Post Message</button>
            <div id="messages"></div>
        </div>
        
        <div class="interactive-section">
            <h2 style="color: var(--nb-orange); margin-bottom: 20px;">Canvas Drawing</h2>
            <p>Click and drag to draw:</p>
            <button class="btn secondary" onclick="clearCanvas()">Clear Canvas</button>
            <canvas id="canvas" width="600" height="300"></canvas>
        </div>
    </div>
    
    <script>
        // NetBird Status Functions
        async function getNetBirdStatus() {
            const loading = document.getElementById('statusLoading');
            const output = document.getElementById('statusOutput');
            
            loading.classList.add('show');
            output.classList.remove('show');
            output.innerHTML = '';
            
            try {
                const response = await fetch('/api/netbird-status');
                const data = await response.json();
                
                if (data.error) {
                    output.innerHTML = '<div class="error">Error: ' + data.error + '</div>';
                } else {
                    output.innerHTML = data.output;
                }
                output.classList.add('show');
            } catch (error) {
                output.innerHTML = '<div class="error">Failed to get NetBird status: ' + error.message + '</div>';
                output.classList.add('show');
            } finally {
                loading.classList.remove('show');
            }
        }
        
        async function getNetBirdStatusSimple() {
            const loading = document.getElementById('statusLoading');
            const output = document.getElementById('statusOutput');
            
            loading.classList.add('show');
            output.classList.remove('show');
            output.innerHTML = '';
            
            try {
                const response = await fetch('/api/netbird-status-simple');
                const data = await response.json();
                
                if (data.error) {
                    output.innerHTML = '<div class="error">Error: ' + data.error + '</div>';
                } else {
                    output.innerHTML = data.output;
                }
                output.classList.add('show');
            } catch (error) {
                output.innerHTML = '<div class="error">Failed to get NetBird status: ' + error.message + '</div>';
                output.classList.add('show');
            } finally {
                loading.classList.remove('show');
            }
        }
        
        // Original Functions
        function showAlert() {
            alert('Hello from NetBird Test Server!');
        }
        
        function updateTime() {
            document.getElementById('time').innerHTML = 'Current time: ' + new Date().toLocaleString();
        }
        
        async function fetchData() {
            try {
                const response = await fetch('/api/data');
                const data = await response.json();
                document.getElementById('apiResult').innerHTML = 
                    '<div class="success">Data fetched: ' + JSON.stringify(data, null, 2) + '</div>';
            } catch (error) {
                document.getElementById('apiResult').innerHTML = 
                    '<div class="error">Error: ' + error.message + '</div>';
            }
        }
        
        function handleSubmit(event) {
            event.preventDefault();
            const name = document.getElementById('nameInput').value;
            document.getElementById('formResult').innerHTML = 
                '<div class="success">Hello, ' + name + '! Form submitted successfully.</div>';
            document.getElementById('nameInput').value = '';
        }
        
        let messageCount = 0;
        function postMessage() {
            const input = document.getElementById('messageInput');
            const message = input.value.trim();
            if (message) {
                const messagesDiv = document.getElementById('messages');
                const messageDiv = document.createElement('div');
                messageDiv.className = 'message';
                messageDiv.innerHTML = 
                    '<span class="timestamp">' + new Date().toLocaleTimeString() + '</span>' +
                    '<span>' + message + '</span>';
                messagesDiv.insertBefore(messageDiv, messagesDiv.firstChild);
                input.value = '';
                messageCount++;
                if (messageCount > 10) {
                    messagesDiv.removeChild(messagesDiv.lastChild);
                }
            }
        }
        
        // Canvas drawing
        const canvas = document.getElementById('canvas');
        const ctx = canvas.getContext('2d');
        let isDrawing = false;
        
        canvas.addEventListener('mousedown', (e) => {
            isDrawing = true;
            const rect = canvas.getBoundingClientRect();
            ctx.beginPath();
            ctx.moveTo(e.clientX - rect.left, e.clientY - rect.top);
        });
        
        canvas.addEventListener('mousemove', (e) => {
            if (isDrawing) {
                const rect = canvas.getBoundingClientRect();
                ctx.lineTo(e.clientX - rect.left, e.clientY - rect.top);
                ctx.strokeStyle = '#f68330';
                ctx.lineWidth = 2;
                ctx.stroke();
            }
        });
        
        canvas.addEventListener('mouseup', () => {
            isDrawing = false;
        });
        
        canvas.addEventListener('mouseleave', () => {
            isDrawing = false;
        });
        
        function clearCanvas() {
            ctx.clearRect(0, 0, canvas.width, canvas.height);
        }
        
        // Initial time update
        updateTime();
    </script>
</body>
</html>
`

const page1HTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Page 1 - NetBird Test</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            background: #181a1d;
            color: #e4e7e9;
            padding: 20px;
        }
        h1 { color: #f68330; }
        a { color: #31e4f5; }
    </style>
</head>
<body>
    <h1>Page 1</h1>
    <p>This is page 1 of the test server.</p>
    <p><a href="/">Back to Home</a></p>
    <p><a href="/page2">Go to Page 2</a></p>
</body>
</html>
`

const page2HTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Page 2 - NetBird Test</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            background: #181a1d;
            color: #e4e7e9;
            padding: 20px;
        }
        h1 { color: #f68330; }
        a { color: #31e4f5; }
    </style>
</head>
<body>
    <h1>Page 2</h1>
    <p>This is page 2 of the test server.</p>
    <p><a href="/">Back to Home</a></p>
    <p><a href="/page1">Go to Page 1</a></p>
</body>
</html>
`

const filesHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Files - NetBird Test</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            background: #181a1d;
            color: #e4e7e9;
            padding: 20px;
        }
        h1 { color: #f68330; }
        a { color: #31e4f5; text-decoration: none; }
        ul { list-style: none; padding: 0; }
        li { padding: 10px; background: #32363d; margin: 5px 0; border-radius: 4px; }
        li:hover { background: #3f444b; }
    </style>
</head>
<body>
    <h1>File Browser</h1>
    <ul>
        <li><a href="/">← Back to Home</a></li>
        <li><a href="test.txt">test.txt</a></li>
        <li><a href="data.json">data.json</a></li>
    </ul>
</body>
</html>
`

func main() {
	// Print startup banner
	fmt.Println("🌐 NetBird Test Server")
	fmt.Println("=====================")
	fmt.Println()
	fmt.Println("This server provides a feature-rich test website for the NetBird internal browser.")
	fmt.Println()
	fmt.Println("Features:")
	fmt.Println("  • Real-time clock and dynamic content")
	fmt.Println("  • Multiple pages with navigation")
	fmt.Println("  • Interactive JavaScript demos")
	fmt.Println("  • REST API endpoints")
	fmt.Println("  • Form submissions")
	fmt.Println("  • Canvas drawing")
	fmt.Println("  • File browser simulation")
	fmt.Println("  • NetBird status monitoring")
	fmt.Println()
	fmt.Println("Starting server on port 8080...")
	fmt.Println()

	tmpl := template.Must(template.New("index").Parse(htmlTemplate))

	// Main page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		tmpl.Execute(w, nil)
	})

	// Other pages
	http.HandleFunc("/page1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page1HTML)
	})

	http.HandleFunc("/page2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page2HTML)
	})

	http.HandleFunc("/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, filesHTML)
	})

	// NetBird Status API endpoints
	http.HandleFunc("/api/netbird-status", func(w http.ResponseWriter, r *http.Request) {
		// Add CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Run netbird status -d command
		cmd := exec.Command("/home/vma/netbird", "status", "-d")
		output, err := cmd.CombinedOutput()

		response := make(map[string]interface{})
		if err != nil {
			response["error"] = fmt.Sprintf("Command failed: %v", err)
			response["output"] = string(output)
		} else {
			response["output"] = string(output)
			response["success"] = true
		}

		json.NewEncoder(w).Encode(response)
	})

	http.HandleFunc("/api/netbird-status-simple", func(w http.ResponseWriter, r *http.Request) {
		// Add CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Run netbird status command (without -d flag)
		cmd := exec.Command("/home/vma/netbird", "status")
		output, err := cmd.CombinedOutput()

		response := make(map[string]interface{})
		if err != nil {
			response["error"] = fmt.Sprintf("Command failed: %v", err)
			response["output"] = string(output)
		} else {
			response["output"] = string(output)
			response["success"] = true
		}

		json.NewEncoder(w).Encode(response)
	})

	// API endpoint
	http.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		data := map[string]interface{}{
			"message":   "Hello from NetBird Test Server API",
			"timestamp": time.Now().Unix(),
			"status":    "active",
			"peers":     3,
		}
		json.NewEncoder(w).Encode(data)
	})

	// File serving
	http.HandleFunc("/test.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "This is a test text file.\nIt contains multiple lines.\nServed by NetBird Test Server.")
	})

	http.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name":"NetBird","version":"0.1.0","features":["ssh","rdp","web"]}`)
	})

	port := ":8080"
	log.Printf("Starting NetBird test server on port 8080")
	log.Println("Access URLs:")
	log.Printf("  http://localhost%s", port)
	log.Println("  http://<hostname>.nb.internal:8080")

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}
