# License Plate Recognition System

A real-time license plate recognition system built in Go using OpenCV (GoCV) and Tesseract OCR. This system captures video from a camera, detects Brazilian license plates, and verifies them against an authorized list for access control.

![Preview of Platerecon 1](images/1.jpg)


## Features

- **Real-time Detection**: Live camera feed processing for license plate recognition
- **Brazilian Plate Support**: Supports both old format (ABC1234) and Mercosul format (ABC1D23) plates
- **OCR Integration**: Uses Tesseract OCR with fallback to simple pattern recognition
- **Access Control**: Maintains a whitelist of authorized license plates
- **Debug Mode**: Visual debugging with bounding boxes and confidence scores
- **Robust Processing**: Advanced image preprocessing for better OCR accuracy

## Prerequisites

### Dependencies

- **Go** (1.19 or higher)
- **OpenCV** (4.x)
- **Tesseract OCR** (optional but recommended)
- **GoCV** - Go bindings for OpenCV
- **gosseract** - Go bindings for Tesseract

### System Requirements

- Camera (webcam or USB camera)
- Linux, macOS, or Windows
- Minimum 4GB RAM recommended

## Installation

### Option 1: Using DevContainer (Recommended) 🐳

The easiest way to run this project is using the provided DevContainer configuration. The system has been tested on **Debian 12** with camera passthrough to the container.

#### Prerequisites:
- Docker
- VS Code with Dev Containers extension
- Camera connected to host system (`/dev/video0`)

#### Setup:
1. Clone the repository:
```bash
git clone https://github.com/raykavin/platerecon-go
cd platerecon
```

2. Open in VS Code and select "Reopen in Container" when prompted, or:
```bash
code .
# Then: Ctrl+Shift+P -> "Dev Containers: Reopen in Container"
```

3. The container will automatically:
   - Install Go 1.24
   - Install OpenCV and GoCV
   - Install Tesseract OCR
   - Configure Fish shell
   - Mount your camera device
   - Set up the development environment

4. Build and run:
```bash
go mod tidy
go build -o plate-recognition main.go
./plate-recognition
```

#### DevContainer Features:
- **Pre-configured Environment**: All dependencies installed automatically
- **Camera Support**: Direct access to host camera via device passthrough
- **Display Support**: X11/Wayland forwarding for GUI applications
- **Fish Shell**: Enhanced terminal experience with autocomplete
- **VS Code Integration**: Go extensions and debugging support
- **Tested Environment**: Verified on Debian 12 host systems

#### DevContainer Configuration:

**Dockerfile:**
```dockerfile
# Base image
FROM mcr.microsoft.com/devcontainers/go:1-1.24-bookworm

ENV DEBIAN_FRONTEND=noninteractive
ARG USERNAME=vscode

# Install packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    fish \
    git \
    curl \
    sudo \
    libtesseract-dev \
    libleptonica-dev \
    tesseract-ocr \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# Change shell to fish
RUN chsh -s /usr/bin/fish ${USERNAME}

# Use non-root user
USER ${USERNAME}
WORKDIR /home/${USERNAME}/workspaces/platerecon

# Clone and install GoCV
RUN git clone https://github.com/hybridgroup/gocv.git /tmp/gocv \
    && cd /tmp/gocv \
    && git checkout v0.41.0 \
    && make install \
    && rm -rf /tmp/gocv

# Start in fish shell
CMD ["fish"]
```

**devcontainer.json:**
```json
{
    "name": "Go DevContainer with Fish",
    "remoteUser": "vscode",
    "build": {
        "dockerfile": "Dockerfile",
        "context": ".."
    },
    "workspaceFolder": "/workspaces/platerecon",
    "forwardPorts": [3000],
    "runArgs": [
        "--network=host",
        "--device=/dev/video0",
        "--group-add=video"
    ],
    "containerEnv": {
        "DISPLAY": "${env:DISPLAY}",
        "WAYLAND_DISPLAY": "${env:WAYLAND_DISPLAY}",
        "XDG_RUNTIME_DIR": "${env:XDG_RUNTIME_DIR}"
    },
    "mounts": [
        "source=/run/user/1000/wayland-0,target=/run/user/1000/wayland-0,type=bind",
        "source=${env:XDG_RUNTIME_DIR},target=${env:XDG_RUNTIME_DIR},type=bind"
    ],
    "postCreateCommand": "go mod tidy && git config --global --add safe.directory /workspaces/platerecon",
    "features": {
        "ghcr.io/devcontainers/features/git:1": {}
    },
    "customizations": {
        "vscode": {
            "extensions": [
                "golang.Go",
                "golang.go-nightly",
                "liuchao.go-struct-tag",
                "yokoe.vscode-postfix-go",
                "ms-ceintl.vscode-language-pack-pt-br",
                "streetsidesoftware.code-spell-checker",
                "streetsidesoftware.code-spell-checker-portuguese-brazilian",
                "visualstudioexptteam.vscodeintellicode",
                "redhat.vscode-yaml",
                "teddyandturtle.fish"
            ],
            "settings": {
                "terminal.integrated.defaultProfile.linux": "fish",
                "go.toolsManagement.checkForUpdates": "local",
                "go.useLanguageServer": true,
                "go.gopath": "/go",
                "go.goroot": "/usr/local/go"
            }
        }
    }
}
```

### Option 2: Manual Installation

#### Ubuntu/Debian:
```bash
sudo apt update
sudo apt install libopencv-dev pkg-config
```

#### macOS (using Homebrew):
```bash
brew install opencv pkg-config
```

#### Windows:
Follow the [OpenCV installation guide](https://docs.opencv.org/master/d3/d52/tutorial_windows_install.html)

#### Install Tesseract OCR (Optional)

**Ubuntu/Debian:**
```bash
sudo apt install tesseract-ocr libtesseract-dev
```

**macOS:**
```bash
brew install tesseract
```

**Windows:**
Download from [GitHub Tesseract releases](https://github.com/UB-Mannheim/tesseract/wiki)

#### Install Go Dependencies

```bash
go mod init license-plate-recognition
go get gocv.io/x/gocv
go get github.com/otiai10/gosseract/v2
```

#### Build the Application

```bash
go build -o plate-recognition main.go
```

## Usage

### Basic Usage

```bash
./plate-recognition
```

### Controls

- **'q' or ESC**: Quit the application
- **'d'**: Toggle debug mode (shows detection bounding boxes)

### Configuration

The system can be configured by modifying the following parameters in the code:

```go
// Authorized plates list
allowedPlates: []string{"QFQ4H64", "ABC1234", "BRA2E19"}

// Detection parameters
minConfidence: 30.0        // Minimum OCR confidence threshold
minTextSize: 6            // Minimum text length
waitTime: 1 * time.Second // Time between detections

// Plate geometry constraints
minPlateArea: 5000         // Minimum plate area in pixels
maxPlateArea: 50000        // Maximum plate area in pixels
minAspectRatio: 2.0        // Minimum width/height ratio
maxAspectRatio: 5.5        // Maximum width/height ratio
```

## System Architecture

### Core Components

1. **PlateRecognitionSystem**: Main system controller
2. **PlateDetection**: Represents a detected license plate with confidence
3. **PlateCandidate**: Represents a potential plate region before OCR

### Detection Pipeline

1. **Image Preprocessing**: 
   - Grayscale conversion
   - Bilateral filtering for noise reduction
   - Canny edge detection

2. **Region Detection**:
   - Contour analysis
   - Geometric filtering based on aspect ratio and area
   - Confidence scoring

3. **OCR Processing**:
   - ROI extraction and enhancement
   - Tesseract OCR with custom configuration
   - Text normalization and validation

4. **Plate Validation**:
   - Format verification (Brazilian patterns)
   - Authorization check
   - Access control decision

### Supported Plate Formats

- **Old Brazilian Format**: ABC1234 (3 letters + 4 numbers)
- **Mercosul Format**: ABC1D23 (3 letters + 1 number + 1 letter + 2 numbers)

## Performance Optimization

### Camera Settings

The system automatically configures the camera for optimal performance:
- Resolution: 1280x720
- Frame rate: 30 FPS
- Auto-exposure adjustment

### OCR Optimization

- Custom Tesseract configuration for license plates
- Character whitelist: A-Z, 0-9
- Single-line page segmentation mode
- Morphological operations for text enhancement

## Troubleshooting

### Common Issues

1. **Camera not detected**:
   - Check camera connection
   - Verify camera permissions
   - Try different camera index (modify `gocv.OpenVideoCapture(0)`)

2. **Tesseract not found**:
   - System falls back to simple pattern recognition
   - Install Tesseract OCR for better accuracy

3. **Low detection accuracy**:
   - Improve lighting conditions
   - Adjust `minConfidence` threshold
   - Clean camera lens

### Debug Mode

Enable debug mode by pressing 'd' to see:
- Detection bounding boxes
- Confidence scores
- Processing parameters

## API Reference

### Main Functions

#### `NewPlateRecognitionSystem() *PlateRecognitionSystem`
Creates a new instance of the recognition system with default parameters.

#### `Initialize() error`
Initializes camera, windows, and OCR components.

#### `Run() error`
Starts the main processing loop.

#### `Cleanup()`
Releases all system resources.

### Key Methods

#### `detectPlates(img gocv.Mat) []PlateDetection`
Detects and recognizes license plates in the given image.

#### `verifyPlate(img gocv.Mat) (bool, string)`
Verifies if any detected plate is authorized for access.

#### `isValidPlate(text string) (bool, string)`
Validates if text matches Brazilian license plate patterns.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [GoCV](https://gocv.io/) - Go bindings for OpenCV
- [gosseract](https://github.com/otiai10/gosseract) - Go bindings for Tesseract
- [OpenCV](https://opencv.org/) - Computer vision library
- [Tesseract OCR](https://github.com/tesseract-ocr/tesseract) - OCR engine

## Future Implementations

The following features are planned for future releases:

### 🔗 IP Camera Support
- **RTSP Stream Integration**: Connect to IP cameras using RTSP protocol
- **Multiple Camera Sources**: Support for multiple camera feeds simultaneously
- **Network Camera Discovery**: Automatic detection of cameras on the network
- **Camera Authentication**: Support for username/password protected cameras
- **Stream Quality Control**: Adaptive streaming based on network conditions

### 🚀 Webhook Integration
- **Real-time Notifications**: Send HTTP webhooks when authorized plates are detected
- **Configurable Endpoints**: Support for multiple webhook URLs
- **Event Payload**: Rich JSON payload with plate information, timestamp, and confidence
- **Retry Mechanism**: Automatic retry for failed webhook deliveries
- **Authentication Support**: Bearer token and API key authentication for webhooks

#### Example Webhook Payload:
```json
{
  "event": "plate_authorized",
  "timestamp": "2025-06-03T14:30:00Z",
  "plate": {
    "number": "QFQ4H64",
    "confidence": 85.3,
    "format": "Old Brazilian format"
  },
  "camera": {
    "id": "cam_001",
    "location": "Main Entrance"
  },
  "access_granted": true
}
```

### 📊 Additional Planned Features
- **Database Integration**: Store detection history and analytics
- **REST API**: HTTP API for external system integration
- **Mobile App**: Remote monitoring and configuration
- **Cloud Storage**: Automatic image backup of detected plates
- **Machine Learning**: Improved accuracy with custom trained models
- **Multi-language Support**: OCR support for different countries' plates

### 🛠️ Configuration Examples

#### IP Camera Configuration (Planned)
```go
// Future IP camera configuration
cameras := []CameraConfig{
    {
        ID: "cam_001",
        URL: "rtsp://admin:password@192.168.1.100:554/stream1",
        Location: "Main Entrance",
        Active: true,
    },
    {
        ID: "cam_002", 
        URL: "rtsp://user:pass@192.168.1.101:554/live",
        Location: "Parking Lot",
        Active: true,
    },
}
```

#### Webhook Configuration (Planned)
```go
// Future webhook configuration
webhooks := []WebhookConfig{
    {
        URL: "https://api.company.com/access-control/webhook",
        Events: []string{"plate_authorized", "plate_denied"},
        Headers: map[string]string{
            "Authorization": "Bearer your-token-here",
            "Content-Type": "application/json",
        },
        RetryAttempts: 3,
        Timeout: 5000, // milliseconds
    },
}
```

**Contribution Welcome**: If you're interested in implementing any of these features, please check our [contributing guidelines](#contributing) and open an issue to discuss the implementation approach.

## 📄License

MIT License © [Raykavin Meireles](https://github.com/raykavin)

Platerecon is licensed under the MIT License. See the [LICENSE](LICENSE.md) file for details.

---
## 📬 Contact

Feel free to reach out for support or collaboration:  
**Email**: [raykavin.meireles@gmail.com](mailto:raykavin.meireles@gmail.com)  
**GitHub**: [@raykavin](https://github.com/raykavin)\
**LinkedIn**: [@raykavin.dev](https://www.linkedin.com/in/raykavin-dev)\
**Instagram**: [@raykavin.dev](https://www.instagram.com/raykavin.dev)


---

**Note**: This system is designed for Brazilian license plates. For other countries, modify the regex patterns and validation logic accordingly.

