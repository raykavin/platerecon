package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/otiai10/gosseract/v2"
	"gocv.io/x/gocv"
)

// PlateDetection represents a detected license plate
type PlateDetection struct {
	Text        string
	Confidence  float64
	BoundingBox image.Rectangle
	Area        int
}

// PlateCandidate represents a potential plate region
type PlateCandidate struct {
	Contour     gocv.PointVector
	BoundingBox image.Rectangle
	AspectRatio float64
	Area        int
	Confidence  float64
}

// PlateRecognitionSystem main system structure
type PlateRecognitionSystem struct {
	camera           *gocv.VideoCapture
	window           *gocv.Window
	debugWindow      *gocv.Window
	allowedPlates    []string
	waitTime         time.Duration
	lastVerification time.Time
	accessGranted    bool
	detectedPlate    string

	// Detection parameters
	minConfidence       float64
	minTextSize         int
	brazilianPlateRegex *regexp.Regexp
	mercosulPlateRegex  *regexp.Regexp

	// Plate geometry constraints
	minPlateArea   int
	maxPlateArea   int
	minAspectRatio float64
	maxAspectRatio float64

	// OCR client
	ocrClient *gosseract.Client

	// Debug mode
	debugMode bool
}

// Interface colors
var (
	WHITE  = color.RGBA{255, 255, 255, 255}
	GREEN  = color.RGBA{0, 255, 0, 255}
	RED    = color.RGBA{0, 0, 255, 255}
	BLUE   = color.RGBA{255, 0, 0, 255}
	BLACK  = color.RGBA{0, 0, 0, 128}
	YELLOW = color.RGBA{0, 255, 255, 255}
)

// NewPlateRecognitionSystem initializes the system
func NewPlateRecognitionSystem() *PlateRecognitionSystem {
	system := &PlateRecognitionSystem{
		allowedPlates:    []string{"QFQ4H64", "ABC1234", "BRA2E19"},
		waitTime:         1 * time.Second,
		lastVerification: time.Now(),
		accessGranted:    false,
		detectedPlate:    "SCANNING...",
		minConfidence:    30.0,
		minTextSize:      6,

		// Brazilian plate dimensions (approximate ratios)
		minPlateArea:   5000,  // Minimum area in pixels
		maxPlateArea:   50000, // Maximum area in pixels
		minAspectRatio: 2.0,   // Width/Height ratio
		maxAspectRatio: 5.5,   // Width/Height ratio

		debugMode: true,
	}

	// Regex patterns for Brazilian license plates
	// Old pattern: ABC1234
	system.brazilianPlateRegex = regexp.MustCompile(`^[A-Z]{3}[0-9]{4}$`)
	// Mercosul pattern: ABC1D23
	system.mercosulPlateRegex = regexp.MustCompile(`^[A-Z]{3}[0-9][A-Z][0-9]{2}$`)

	return system
}

// Initialize camera, windows, and OCR
func (sys *PlateRecognitionSystem) Initialize() error {
	var err error

	// Initialize camera
	sys.camera, err = gocv.OpenVideoCapture(0)
	if err != nil {
		return fmt.Errorf("error opening camera: %v", err)
	}

	// Set camera properties for better quality
	sys.camera.Set(gocv.VideoCaptureFrameWidth, 1280)
	sys.camera.Set(gocv.VideoCaptureFrameHeight, 720)
	sys.camera.Set(gocv.VideoCaptureFPS, 30)
	sys.camera.Set(gocv.VideoCaptureAutoExposure, 0.25)

	// Initialize windows
	sys.window = gocv.NewWindow("License Plate Recognition System")
	sys.window.ResizeWindow(1280, 720)

	if sys.debugMode {
		sys.debugWindow = gocv.NewWindow("Debug - Plate Detection")
		sys.debugWindow.ResizeWindow(640, 480)
	}

	// Initialize OCR client with better settings
	sys.ocrClient = gosseract.NewClient()
	if err := sys.ocrClient.SetLanguage("eng"); err != nil {
		fmt.Printf("Aviso: Erro ao configurar OCR Tesseract: %v\n", err)
		fmt.Println("Continuando com detecção de padrão simples...")
		sys.ocrClient.Close()
		sys.ocrClient = nil
	} else {
		// Configure OCR for license plates
		sys.ocrClient.SetPageSegMode(gosseract.PSM_SINGLE_LINE)
		sys.ocrClient.SetVariable("tessedit_char_whitelist", "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
		sys.ocrClient.SetVariable("tessedit_unrej_any_wd", "1")

		// Configurações adicionais para melhor precisão
		sys.ocrClient.SetVariable("load_system_dawg", "0")
		sys.ocrClient.SetVariable("load_freq_dawg", "0")
		sys.ocrClient.SetVariable("load_unambig_dawg", "0")
		sys.ocrClient.SetVariable("load_punc_dawg", "0")
		sys.ocrClient.SetVariable("load_number_dawg", "0")
		sys.ocrClient.SetVariable("load_bigram_dawg", "0")

		fmt.Println("OCR Tesseract inicializado com sucesso")
	}

	return nil
}

// Cleanup releases resources
func (sys *PlateRecognitionSystem) Cleanup() {
	if sys.camera != nil {
		sys.camera.Close()
	}
	if sys.window != nil {
		sys.window.Close()
	}
	if sys.debugWindow != nil {
		sys.debugWindow.Close()
	}
	if sys.ocrClient != nil {
		sys.ocrClient.Close()
	}
}

// normalizeText removes spaces and normalizes characters
func (sys *PlateRecognitionSystem) normalizeText(text string) string {
	// Remove spaces and convert to uppercase
	text = strings.ToUpper(strings.ReplaceAll(text, " ", ""))

	// Remove common OCR artifacts
	replacer := strings.NewReplacer(
		"-", "",
		".", "",
		"_", "",
		"|", "I",
		"1", "I", // In letter context
		"0", "O", // In letter context for some cases
	)
	text = replacer.Replace(text)

	// Remove non-alphanumeric characters
	var result strings.Builder
	for _, char := range text {
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			result.WriteRune(char)
		}
	}

	return result.String()
}

// isValidPlate validates if text matches Brazilian license plate patterns
func (sys *PlateRecognitionSystem) isValidPlate(text string) (bool, string) {
	text = sys.normalizeText(text)

	// Check minimum size
	if len(text) < sys.minTextSize || len(text) > 8 {
		return false, "Invalid length"
	}

	// Check old Brazilian plate pattern (ABC1234)
	if sys.brazilianPlateRegex.MatchString(text) && len(text) == 7 {
		return true, "Old Brazilian format"
	}

	// Check Mercosul pattern (ABC1D23)
	if sys.mercosulPlateRegex.MatchString(text) && len(text) == 7 {
		return true, "Mercosul format"
	}

	return false, "Invalid pattern"
}

// findPlateRegions detects potential license plate regions using image processing
func (sys *PlateRecognitionSystem) findPlateRegions(img gocv.Mat) []PlateCandidate {
	var candidates []PlateCandidate

	// Convert to grayscale
	gray := gocv.NewMat()
	defer gray.Close()
	gocv.CvtColor(img, &gray, gocv.ColorBGRToGray)

	// Apply bilateral filter to reduce noise while keeping edges sharp
	filtered := gocv.NewMat()
	defer filtered.Close()
	gocv.BilateralFilter(gray, &filtered, 11, 17, 17)

	// Find edges using Canny
	edges := gocv.NewMat()
	defer edges.Close()
	gocv.Canny(filtered, &edges, 30, 200)

	// Apply morphological operations to connect text regions
	kernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(3, 3))
	defer kernel.Close()

	dilated := gocv.NewMat()
	defer dilated.Close()
	gocv.Dilate(edges, &dilated, kernel)

	// Find contours
	contours := gocv.FindContours(dilated, gocv.RetrievalExternal, gocv.ChainApproxSimple)
	defer contours.Close()

	// Analyze each contour
	for i := 0; i < contours.Size(); i++ {
		contour := contours.At(i)

		// Get bounding rectangle
		boundingRect := gocv.BoundingRect(contour)

		// Calculate properties
		area := boundingRect.Dx() * boundingRect.Dy()
		aspectRatio := float64(boundingRect.Dx()) / float64(boundingRect.Dy())

		// Filter by geometric constraints typical of license plates
		if area >= sys.minPlateArea && area <= sys.maxPlateArea &&
			aspectRatio >= sys.minAspectRatio && aspectRatio <= sys.maxAspectRatio &&
			boundingRect.Dx() > 100 && boundingRect.Dy() > 20 {

			candidate := PlateCandidate{
				Contour:     contour,
				BoundingBox: boundingRect,
				AspectRatio: aspectRatio,
				Area:        area,
				Confidence:  sys.calculateRegionConfidence(gray, boundingRect),
			}

			candidates = append(candidates, candidate)
		}
	}

	// Sort candidates by confidence
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Confidence > candidates[j].Confidence
	})

	// Return top candidates
	maxCandidates := 5
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	return candidates
}

// calculateRegionConfidence estimates how likely a region contains a license plate
func (sys *PlateRecognitionSystem) calculateRegionConfidence(gray gocv.Mat, rect image.Rectangle) float64 {
	confidence := 0.0

	// Extract ROI
	roi := gray.Region(rect)
	defer roi.Close()

	// Check for horizontal text-like patterns
	// Apply threshold
	thresh := gocv.NewMat()
	defer thresh.Close()
	gocv.Threshold(roi, &thresh, 0, 255, gocv.ThresholdBinary+gocv.ThresholdOtsu)

	// Count transitions (black to white) in horizontal direction (typical of text)
	width := thresh.Cols()
	height := thresh.Rows()

	if height > 0 && width > 0 {
		// Sample middle row
		middleRow := height / 2
		transitions := 0
		lastPixel := thresh.GetUCharAt(middleRow, 0)

		for x := 1; x < width; x++ {
			currentPixel := thresh.GetUCharAt(middleRow, x)
			if currentPixel != lastPixel {
				transitions++
			}
			lastPixel = currentPixel
		}

		// More transitions suggest text-like content
		confidence += float64(transitions) / float64(width) * 100

		// Aspect ratio bonus (license plates have specific ratios)
		aspectRatio := float64(rect.Dx()) / float64(rect.Dy())
		if aspectRatio >= 2.5 && aspectRatio <= 4.5 {
			confidence += 20
		}

		// Size bonus (reasonable plate size)
		area := rect.Dx() * rect.Dy()
		if area >= 8000 && area <= 25000 {
			confidence += 15
		}
	}

	return math.Min(confidence, 100)
}

// estimateTextConfidence provides a confidence estimation based on text characteristics
func (sys *PlateRecognitionSystem) estimateTextConfidence(text string) float64 {
	if len(text) == 0 {
		return 0
	}

	confidence := 30.0 // Base confidence

	// Length bonus (Brazilian plates are 7 characters)
	if len(text) == 7 {
		confidence += 35
	} else if len(text) >= 6 && len(text) <= 8 {
		confidence += 20
	} else if len(text) < 4 {
		confidence -= 20 // Penalize very short text
	}

	// Alphanumeric character ratio
	alphanumeric := 0
	letters := 0
	numbers := 0

	for _, char := range text {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
			alphanumeric++
			letters++
		} else if char >= '0' && char <= '9' {
			alphanumeric++
			numbers++
		}
	}

	if len(text) > 0 {
		ratio := float64(alphanumeric) / float64(len(text))
		confidence += ratio * 20

		// Brazilian plates should have both letters and numbers
		if letters > 0 && numbers > 0 {
			confidence += 15
		}
	}

	// Pattern matching bonus
	normalizedText := sys.normalizeText(text)
	if valid, reason := sys.isValidPlate(normalizedText); valid {
		confidence += 25
		if reason == "Old Brazilian format" || reason == "Mercosul format" {
			confidence += 5 // Extra bonus for exact match
		}
	}

	// Penalize if text has too many special characters or spaces
	specialChars := len(text) - alphanumeric
	if specialChars > 2 {
		confidence -= float64(specialChars) * 5
	}

	// Character quality assessment (penalize common OCR errors)
	errorChars := strings.Count(strings.ToUpper(text), "I") +
		strings.Count(strings.ToUpper(text), "L") +
		strings.Count(strings.ToUpper(text), "O")

	if errorChars > len(text)/2 {
		confidence -= 10 // Likely OCR confusion
	}

	return math.Max(math.Min(confidence, 100), 0)
}

// performOCR executes OCR on a specific region with improved error handling
func (sys *PlateRecognitionSystem) performOCR(img gocv.Mat, region image.Rectangle) (string, float64) {
	// Fallback to simple OCR if Tesseract client is not available
	if sys.ocrClient == nil {
		return sys.performSimpleOCR(img, region)
	}

	// Extract and preprocess the region
	roi := img.Region(region)
	defer roi.Close()

	// Resize for better OCR (license plates are usually small)
	resized := gocv.NewMat()
	defer resized.Close()
	gocv.Resize(roi, &resized, image.Pt(0, 0), 3.0, 3.0, gocv.InterpolationCubic)

	// Convert to grayscale if needed
	processed := gocv.NewMat()
	defer processed.Close()

	if resized.Channels() > 1 {
		gocv.CvtColor(resized, &processed, gocv.ColorBGRToGray)
	} else {
		resized.CopyTo(&processed)
	}

	// Apply adaptive threshold for better text contrast
	gocv.AdaptiveThreshold(processed, &processed, 255, gocv.AdaptiveThresholdGaussian, gocv.ThresholdBinary, 11, 2)

	// Additional morphological operations to clean up text
	kernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(2, 2))
	defer kernel.Close()
	gocv.MorphologyEx(processed, &processed, gocv.MorphClose, kernel)

	// Convert Mat to bytes for Tesseract
	buf, err := gocv.IMEncode(".png", processed)
	if err != nil {
		fmt.Printf("Erro ao codificar imagem: %v\n", err)
		return "", 0
	}
	defer buf.Close()

	// Perform OCR
	err = sys.ocrClient.SetImageFromBytes(buf.GetBytes())
	if err != nil {
		fmt.Printf("Erro ao definir imagem OCR: %v\n", err)
		return "", 0
	}

	text, err := sys.ocrClient.Text()
	if err != nil {
		fmt.Printf("Erro ao extrair texto OCR: %v\n", err)
		return "", 0
	}

	// Clean the text
	text = strings.TrimSpace(text)

	// Use our own confidence estimation based on text characteristics
	// This is more reliable across different gosseract versions
	confidence := sys.estimateTextConfidence(text)

	// Additional confidence boost if text looks like a valid plate
	normalizedText := sys.normalizeText(text)
	if valid, _ := sys.isValidPlate(normalizedText); valid {
		confidence = math.Min(confidence+15, 95) // Cap at 95% to remain realistic
	}

	return text, confidence
}

// Alternative OCR approach using template matching (fallback)
func (sys *PlateRecognitionSystem) performSimpleOCR(img gocv.Mat, region image.Rectangle) (string, float64) {
	roi := img.Region(region)
	defer roi.Close()

	// Basic text detection using morphological operations
	gray := gocv.NewMat()
	defer gray.Close()

	if roi.Channels() > 1 {
		gocv.CvtColor(roi, &gray, gocv.ColorBGRToGray)
	} else {
		roi.CopyTo(&gray)
	}

	// Apply threshold
	thresh := gocv.NewMat()
	defer thresh.Close()
	gocv.Threshold(gray, &thresh, 0, 255, gocv.ThresholdBinary+gocv.ThresholdOtsu)

	// Analyze text patterns
	confidence := sys.analyzeTextPattern(thresh)

	if confidence > 50 {
		// Return a placeholder for regions that look like text
		// In production, you might want to use a cloud OCR service here
		return "DETECTADO", confidence
	}

	return "", 0
}

// analyzeTextPattern provides basic text pattern analysis
func (sys *PlateRecognitionSystem) analyzeTextPattern(img gocv.Mat) float64 {
	if img.Empty() {
		return 0
	}

	// Count horizontal line segments (typical of text)
	height := img.Rows()
	width := img.Cols()

	if height == 0 || width == 0 {
		return 0
	}

	// Sample middle region
	midY := height / 2
	lineSegments := 0
	inSegment := false

	for x := 0; x < width; x++ {
		pixel := img.GetUCharAt(midY, x)

		if pixel > 128 { // White pixel
			if !inSegment {
				lineSegments++
				inSegment = true
			}
		} else {
			inSegment = false
		}
	}

	// More segments suggest text-like structure
	confidence := float64(lineSegments) * 8
	return math.Min(confidence, 100)
}

// detectPlates finds and recognizes license plates in the image
func (sys *PlateRecognitionSystem) detectPlates(img gocv.Mat) []PlateDetection {
	var detections []PlateDetection

	// Find potential plate regions
	candidates := sys.findPlateRegions(img)

	// Process each candidate region with OCR
	for _, candidate := range candidates {
		text, confidence := sys.performOCR(img, candidate.BoundingBox)

		if confidence >= sys.minConfidence && len(text) > 0 {
			detection := PlateDetection{
				Text:        text,
				Confidence:  confidence,
				BoundingBox: candidate.BoundingBox,
				Area:        candidate.Area,
			}
			detections = append(detections, detection)
		}
	}

	// Sort by confidence
	sort.Slice(detections, func(i, j int) bool {
		return detections[i].Confidence > detections[j].Confidence
	})

	return detections
}

// verifyPlate checks for license plates in the image
func (sys *PlateRecognitionSystem) verifyPlate(img gocv.Mat) (bool, string) {
	detections := sys.detectPlates(img)

	for _, detection := range detections {
		fmt.Printf("OCR Result: '%s' (Confidence: %.1f%%)\n", detection.Text, detection.Confidence)

		// Normalize the detected text
		normalizedText := sys.normalizeText(detection.Text)

		// Skip if too short after normalization
		if len(normalizedText) < 6 {
			continue
		}

		// Validate plate format
		isValid, reason := sys.isValidPlate(normalizedText)
		if !isValid {
			fmt.Printf("Invalid plate format (%s): %s\n", reason, normalizedText)
			continue
		}

		fmt.Printf("Valid plate detected: %s (%s)\n", normalizedText, reason)

		// Check if it's in the allowed list
		for _, allowedPlate := range sys.allowedPlates {
			if normalizedText == strings.ToUpper(allowedPlate) {
				return true, normalizedText
			}
		}

		// Return the first valid plate found, even if not allowed
		return false, normalizedText
	}

	return false, "SCANNING..."
}

// drawDetections draws bounding boxes around detected regions
func (sys *PlateRecognitionSystem) drawDetections(img *gocv.Mat) {
	if !sys.debugMode {
		return
	}

	// Find candidates for visualization
	candidates := sys.findPlateRegions(*img)

	// Draw bounding boxes for all candidates
	for i, candidate := range candidates {
		color := BLUE
		if i == 0 { // Best candidate
			color = YELLOW
		}

		gocv.Rectangle(img, candidate.BoundingBox, color, 2)

		// Draw confidence text
		text := fmt.Sprintf("%.1f%%", candidate.Confidence)
		gocv.PutText(img, text,
			image.Pt(candidate.BoundingBox.Min.X, candidate.BoundingBox.Min.Y-10),
			gocv.FontHersheySimplex, 0.5, color, 1)
	}
}

// drawInterface draws the main interface
func (sys *PlateRecognitionSystem) drawInterface(img *gocv.Mat) {
	// Draw semi-transparent background
	gocv.Rectangle(img, image.Rect(0, 0, 900, 150), BLACK, -1)

	// Title
	gocv.PutText(img, "SISTEMA DE RECONHECIMENTO DE PLACAS",
		image.Pt(20, 30), gocv.FontHersheySimplex, 0.7, WHITE, 2)

	// Allowed plates
	platesStr := strings.Join(sys.allowedPlates, ", ")
	gocv.PutText(img, fmt.Sprintf("Placas Permitidas: %s", platesStr),
		image.Pt(20, 55), gocv.FontHersheySimplex, 0.6, WHITE, 1)

	// Detected plate
	detectedColor := RED
	if sys.accessGranted {
		detectedColor = GREEN
	}
	gocv.PutText(img, fmt.Sprintf("Placa Detectada: %s", sys.detectedPlate),
		image.Pt(20, 80), gocv.FontHersheySimplex, 0.6, detectedColor, 2)

	// Access status
	status := "ACESSO NEGADO"
	if sys.accessGranted {
		status = "ACESSO LIBERADO"
	}
	gocv.PutText(img, status,
		image.Pt(20, 105), gocv.FontHersheySimplex, 0.7, detectedColor, 2)

	// Debug info
	ocrStatus := "Tesseract"
	if sys.ocrClient == nil {
		ocrStatus = "Simples"
	}
	gocv.PutText(img, fmt.Sprintf("OCR: %s | Confiança Min: %.0f%% | Última: %s",
		ocrStatus, sys.minConfidence, sys.lastVerification.Format("15:04:05")),
		image.Pt(20, 130), gocv.FontHersheySimplex, 0.4, WHITE, 1)
}

// Run executes the main system loop
func (sys *PlateRecognitionSystem) Run() error {
	img := gocv.NewMat()
	defer img.Close()

	debugImg := gocv.NewMat()
	defer debugImg.Close()

	fmt.Println("=== SISTEMA DE RECONHECIMENTO DE PLACAS ===")
	fmt.Println("Pressione 'q' para sair")
	fmt.Println("Pressione 'd' para alternar modo debug")
	fmt.Printf("Placas permitidas: %s\n", strings.Join(sys.allowedPlates, ", "))
	fmt.Printf("Confiança mínima OCR: %.0f%%\n", sys.minConfidence)

	if sys.ocrClient != nil {
		fmt.Println("OCR: Tesseract ativo")
	} else {
		fmt.Println("OCR: Modo simples (Tesseract não disponível)")
	}
	fmt.Println("============================================")

	for {
		// Capture frame
		if ok := sys.camera.Read(&img); !ok {
			fmt.Println("Erro ao capturar frame da câmera")
			break
		}

		if img.Empty() {
			continue
		}

		// Flip horizontally for mirror effect
		gocv.Flip(img, &img, 1)

		// Check for new plate detection
		if time.Since(sys.lastVerification) > sys.waitTime {
			sys.accessGranted, sys.detectedPlate = sys.verifyPlate(img)
			sys.lastVerification = time.Now()

			if sys.accessGranted {
				fmt.Printf("✅ ACESSO LIBERADO - Placa: %s\n", sys.detectedPlate)
			} else if sys.detectedPlate != "SCANNING..." {
				fmt.Printf("❌ ACESSO NEGADO - Placa: %s (não autorizada)\n", sys.detectedPlate)
			}
		}

		// Draw detection boxes (debug mode)
		if sys.debugMode {
			sys.drawDetections(&img)
		}

		// Draw interface
		sys.drawInterface(&img)

		// Show main window
		sys.window.IMShow(img)

		// Show debug window if enabled
		if sys.debugMode && sys.debugWindow != nil {
			// Create debug view (grayscale + processed)
			gray := gocv.NewMat()
			gocv.CvtColor(img, &gray, gocv.ColorBGRToGray)
			gocv.Resize(gray, &debugImg, image.Pt(640, 480), 0, 0, gocv.InterpolationLinear)
			sys.debugWindow.IMShow(debugImg)
			gray.Close()
		}

		// Handle keyboard input
		key := sys.window.WaitKey(1)
		switch key {
		case 'q', 'Q', 27: // Quit
			return nil
		case 'd', 'D': // Toggle debug mode
			sys.debugMode = !sys.debugMode
			fmt.Printf("Modo debug: %t\n", sys.debugMode)
			if !sys.debugMode && sys.debugWindow != nil {
				sys.debugWindow.Close()
				sys.debugWindow = nil
			} else if sys.debugMode && sys.debugWindow == nil {
				sys.debugWindow = gocv.NewWindow("Debug - Plate Detection")
				sys.debugWindow.ResizeWindow(640, 480)
			}
		}
	}

	return nil
}

func main() {
	// Create and initialize system
	system := NewPlateRecognitionSystem()

	if err := system.Initialize(); err != nil {
		log.Fatal("Erro ao inicializar sistema:", err)
	}
	defer system.Cleanup()

	// Run main loop
	if err := system.Run(); err != nil {
		log.Fatal("Erro durante execução:", err)
	}

	fmt.Println("Sistema encerrado.")
}
