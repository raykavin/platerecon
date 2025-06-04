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

	// Frame history for stabilization
	frameHistory    []string
	historySize     int
	stabilizedPlate string
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
		waitTime:         500 * time.Millisecond, // Reduzido para detecção mais rápida
		lastVerification: time.Now(),
		accessGranted:    false,
		detectedPlate:    "SCANNING...",
		minConfidence:    25.0, // Reduzido para capturar mais detecções
		minTextSize:      6,

		// Brazilian plate dimensions (approximate ratios)
		minPlateArea:   3000,  // Reduzido para detectar placas menores
		maxPlateArea:   80000, // Aumentado para placas próximas
		minAspectRatio: 2.0,   // Width/Height ratio
		maxAspectRatio: 5.5,   // Width/Height ratio

		debugMode: true,

		// Frame history
		historySize:     10,
		frameHistory:    make([]string, 0),
		stabilizedPlate: "",
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
	sys.camera.Set(gocv.VideoCaptureFrameWidth, 1920) // Aumentado para maior resolução
	sys.camera.Set(gocv.VideoCaptureFrameHeight, 1080)
	sys.camera.Set(gocv.VideoCaptureFPS, 30)
	sys.camera.Set(gocv.VideoCaptureAutoExposure, 0.25)
	sys.camera.Set(gocv.VideoCaptureContrast, 0.5)
	sys.camera.Set(gocv.VideoCaptureBrightness, 0.5)

	// Initialize windows
	sys.window = gocv.NewWindow("License Plate Recognition System")
	sys.window.ResizeWindow(1280, 720)

	if sys.debugMode {
		sys.debugWindow = gocv.NewWindow("Debug - Plate Detection")
		sys.debugWindow.ResizeWindow(800, 600)
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
		sys.ocrClient.SetPageSegMode(gosseract.PSM_SINGLE_BLOCK)
		sys.ocrClient.SetVariable("tessedit_char_whitelist", "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
		sys.ocrClient.SetVariable("tessedit_unrej_any_wd", "1")
		sys.ocrClient.SetVariable("min_characters_to_try", "4")

		// Desabilitar dicionários para melhor reconhecimento de placas
		sys.ocrClient.SetVariable("load_system_dawg", "0")
		sys.ocrClient.SetVariable("load_freq_dawg", "0")
		sys.ocrClient.SetVariable("load_unambig_dawg", "0")
		sys.ocrClient.SetVariable("load_punc_dawg", "0")
		sys.ocrClient.SetVariable("load_number_dawg", "0")
		sys.ocrClient.SetVariable("load_bigram_dawg", "0")
		sys.ocrClient.SetVariable("wordrec_enable_assoc", "0")

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

// normalizeText removes spaces and normalizes characters with improved OCR corrections
func (sys *PlateRecognitionSystem) normalizeText(text string) string {
	// Remove spaces and convert to uppercase
	text = strings.ToUpper(strings.ReplaceAll(text, " ", ""))

	// Enhanced OCR corrections for common misreads
	replacer := strings.NewReplacer(
		"-", "",
		".", "",
		"_", "",
		",", "",
		"'", "",
		"\"", "",
		"|", "I",
		"!", "I",
		"}", "J",
		"{", "I",
		"[", "I",
		"]", "I",
		"(", "C",
		")", "D",
		"$", "S",
		"@", "Q",
		"#", "H",
		"&", "8",
	)
	text = replacer.Replace(text)

	// Context-aware corrections
	result := []rune(text)
	for i := 0; i < len(result); i++ {
		// Corrigir 0/O e 1/I baseado no contexto
		if i < 3 { // Primeiros 3 caracteres devem ser letras
			if result[i] == '0' {
				result[i] = 'O'
			}
			if result[i] == '1' {
				result[i] = 'I'
			}
		} else if i >= 3 && i < 7 { // Posições 4-7 podem ser números ou letras dependendo do formato
			// Para formato antigo (ABC1234), posições 4-7 são números
			// Para Mercosul (ABC1D23), posição 5 é letra
			if i == 4 && len(result) >= 7 { // Posição 5 (índice 4)
				// Verificar se parece ser formato Mercosul
				if i+2 < len(result) && isLetter(result[i]) && isDigit(result[i+1]) && isDigit(result[i+2]) {
					// Provavelmente Mercosul, manter como letra
					if result[i] == '0' {
						result[i] = 'O'
					}
					if result[i] == '1' {
						result[i] = 'I'
					}
				}
			}
		}
	}

	// Remove non-alphanumeric characters
	var finalResult strings.Builder
	for _, char := range result {
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			finalResult.WriteRune(char)
		}
	}

	return finalResult.String()
}

func isLetter(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
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

// enhanceContrast applies contrast enhancement techniques
func (sys *PlateRecognitionSystem) enhanceContrast(img gocv.Mat) gocv.Mat {
	enhanced := gocv.NewMat()

	// Use histogram equalization for contrast enhancement
	gocv.EqualizeHist(img, &enhanced)

	return enhanced
}

// preprocessForOCR applies multiple preprocessing techniques for better OCR
func (sys *PlateRecognitionSystem) preprocessForOCR(roi gocv.Mat) gocv.Mat {
	processed := gocv.NewMat()

	// 1. Resize for better OCR (3x larger)
	resized := gocv.NewMat()
	defer resized.Close()
	gocv.Resize(roi, &resized, image.Pt(0, 0), 3.0, 3.0, gocv.InterpolationCubic)

	// 2. Convert to grayscale if needed
	gray := gocv.NewMat()
	defer gray.Close()
	if resized.Channels() > 1 {
		gocv.CvtColor(resized, &gray, gocv.ColorBGRToGray)
	} else {
		resized.CopyTo(&gray)
	}

	// 3. Apply bilateral filter to reduce noise while keeping edges
	filtered := gocv.NewMat()
	defer filtered.Close()
	gocv.BilateralFilter(gray, &filtered, 5, 50, 50)

	// 4. Apply contrast enhancement
	enhanced := sys.enhanceContrast(filtered)

	// 5. Apply multiple threshold techniques and choose the best
	thresh1 := gocv.NewMat()
	defer thresh1.Close()
	gocv.Threshold(enhanced, &thresh1, 0, 255, gocv.ThresholdBinary+gocv.ThresholdOtsu)

	thresh2 := gocv.NewMat()
	defer thresh2.Close()
	gocv.AdaptiveThreshold(enhanced, &thresh2, 255, gocv.AdaptiveThresholdGaussian, gocv.ThresholdBinary, 11, 2)

	// 6. Morphological operations to clean up
	kernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(2, 2))
	defer kernel.Close()

	// Try both threshold results
	morph1 := gocv.NewMat()
	defer morph1.Close()
	gocv.MorphologyEx(thresh1, &morph1, gocv.MorphClose, kernel)

	morph2 := gocv.NewMat()
	defer morph2.Close()
	gocv.MorphologyEx(thresh2, &morph2, gocv.MorphClose, kernel)

	// 7. Denoise
	denoised1 := gocv.NewMat()
	defer denoised1.Close()
	gocv.MedianBlur(morph1, &denoised1, 3)

	denoised2 := gocv.NewMat()
	defer denoised2.Close()
	gocv.MedianBlur(morph2, &denoised2, 3)

	// Choose the best result based on character-like features
	score1 := sys.evaluateTextQuality(denoised1)
	score2 := sys.evaluateTextQuality(denoised2)

	if score1 > score2 {
		denoised1.CopyTo(&processed)
	} else {
		denoised2.CopyTo(&processed)
	}

	return processed
}

// evaluateTextQuality evaluates how likely an image contains text
func (sys *PlateRecognitionSystem) evaluateTextQuality(img gocv.Mat) float64 {
	if img.Empty() {
		return 0
	}

	height := img.Rows()
	width := img.Cols()

	// Count transitions in horizontal direction
	transitions := 0
	for y := height / 3; y < 2*height/3; y++ {
		lastPixel := img.GetUCharAt(y, 0)
		for x := 1; x < width; x++ {
			currentPixel := img.GetUCharAt(y, x)
			if currentPixel != lastPixel {
				transitions++
			}
			lastPixel = currentPixel
		}
	}

	// More transitions generally indicate better text separation
	return float64(transitions)
}

// findPlateRegions detects potential license plate regions using multiple techniques
func (sys *PlateRecognitionSystem) findPlateRegions(img gocv.Mat) []PlateCandidate {
	var candidates []PlateCandidate

	// Method 1: Edge-based detection
	candidates1 := sys.findPlatesByEdges(img)
	candidates = append(candidates, candidates1...)

	// Method 2: Color-based detection (for white plates)
	candidates2 := sys.findPlatesByColor(img)
	candidates = append(candidates, candidates2...)

	// Method 3: Text region detection
	candidates3 := sys.findPlatesByTextRegions(img)
	candidates = append(candidates, candidates3...)

	// Remove duplicates and sort by confidence
	candidates = sys.removeDuplicateCandidates(candidates)

	// Sort candidates by confidence
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Confidence > candidates[j].Confidence
	})

	// Return top candidates
	maxCandidates := 10
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	return candidates
}

// findPlatesByEdges uses edge detection to find plates
func (sys *PlateRecognitionSystem) findPlatesByEdges(img gocv.Mat) []PlateCandidate {
	var candidates []PlateCandidate

	// Convert to grayscale
	gray := gocv.NewMat()
	defer gray.Close()
	gocv.CvtColor(img, &gray, gocv.ColorBGRToGray)

	// Apply bilateral filter
	filtered := gocv.NewMat()
	defer filtered.Close()
	gocv.BilateralFilter(gray, &filtered, 11, 17, 17)

	// Find edges using Canny with multiple thresholds
	for _, threshold := range []struct{ low, high float32 }{
		{30, 200}, {50, 150}, {80, 250},
		// {20, 100}, {40, 180}, {60, 220},
	} {
		edges := gocv.NewMat()
		gocv.Canny(filtered, &edges, threshold.low, threshold.high)

		// Apply morphological operations
		kernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(3, 3))
		dilated := gocv.NewMat()
		gocv.Dilate(edges, &dilated, kernel)
		kernel.Close()

		// Find contours
		contours := gocv.FindContours(dilated, gocv.RetrievalExternal, gocv.ChainApproxSimple)

		// Analyze each contour
		for i := range contours.Size() {
			contour := contours.At(i)
			boundingRect := gocv.BoundingRect(contour)
			area := boundingRect.Dx() * boundingRect.Dy()
			aspectRatio := float64(boundingRect.Dx()) / float64(boundingRect.Dy())

			if area >= sys.minPlateArea && area <= sys.maxPlateArea &&
				aspectRatio >= sys.minAspectRatio && aspectRatio <= sys.maxAspectRatio &&
				boundingRect.Dx() > 80 && boundingRect.Dy() > 20 {

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

		contours.Close()
		dilated.Close()
		edges.Close()
	}

	return candidates
}

// findPlatesByColor looks for white rectangular regions (typical of plates)
func (sys *PlateRecognitionSystem) findPlatesByColor(img gocv.Mat) []PlateCandidate {
	var candidates []PlateCandidate

	// Convert to grayscale and threshold for white regions
	gray := gocv.NewMat()
	defer gray.Close()
	gocv.CvtColor(img, &gray, gocv.ColorBGRToGray)

	// Threshold to get bright/white regions
	thresh := gocv.NewMat()
	defer thresh.Close()
	gocv.Threshold(gray, &thresh, 200, 255, gocv.ThresholdBinary)

	// Apply morphological operations to connect regions
	kernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(5, 5))
	defer kernel.Close()

	closed := gocv.NewMat()
	defer closed.Close()
	gocv.MorphologyEx(thresh, &closed, gocv.MorphClose, kernel)

	// Find contours
	contours := gocv.FindContours(closed, gocv.RetrievalExternal, gocv.ChainApproxSimple)
	defer contours.Close()

	for i := 0; i < contours.Size(); i++ {
		contour := contours.At(i)
		boundingRect := gocv.BoundingRect(contour)
		area := boundingRect.Dx() * boundingRect.Dy()
		aspectRatio := float64(boundingRect.Dx()) / float64(boundingRect.Dy())

		if area >= sys.minPlateArea && area <= sys.maxPlateArea &&
			aspectRatio >= sys.minAspectRatio && aspectRatio <= sys.maxAspectRatio {

			// Additional check: verify if region is mostly white
			roi := gray.Region(boundingRect)

			// Sample brightness at several points instead of calculating full mean
			bright := true
			samplePoints := 10
			// threshold := 180
			threshold := uint8(2)

			if roi.Rows() > 0 && roi.Cols() > 0 {
				for i := range samplePoints {
					y := (roi.Rows() * i) / samplePoints
					x := (roi.Cols() * i) / samplePoints
					if y < roi.Rows() && x < roi.Cols() {
						pixelValue := roi.GetUCharAt(y, x)
						if pixelValue < threshold {
							bright = false
							break
						}
					}
				}
			}
			roi.Close()

			// If region is bright, it's likely a white plate
			if bright {
				candidate := PlateCandidate{
					Contour:     contour,
					BoundingBox: boundingRect,
					AspectRatio: aspectRatio,
					Area:        area,
					Confidence:  sys.calculateRegionConfidence(gray, boundingRect) * 0.8,
				}
				candidates = append(candidates, candidate)
			}
		}
	}

	return candidates
}

// findPlatesByTextRegions uses MSER to find text-like regions
func (sys *PlateRecognitionSystem) findPlatesByTextRegions(img gocv.Mat) []PlateCandidate {
	var candidates []PlateCandidate

	// Convert to grayscale
	gray := gocv.NewMat()
	defer gray.Close()
	gocv.CvtColor(img, &gray, gocv.ColorBGRToGray)

	// Use gradient-based approach
	gradX := gocv.NewMat()
	defer gradX.Close()
	gocv.Sobel(gray, &gradX, gocv.MatTypeCV32F, 1, 0, 3, 1, 0, gocv.BorderDefault)

	// Take absolute value and convert back to uint8
	gocv.ConvertScaleAbs(gradX, &gradX, 1, 0)

	// Apply threshold
	thresh := gocv.NewMat()
	defer thresh.Close()
	gocv.Threshold(gradX, &thresh, 0, 255, gocv.ThresholdBinary+gocv.ThresholdOtsu)

	// Morphological operations to connect text regions
	kernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(20, 3))
	defer kernel.Close()
	gocv.MorphologyEx(thresh, &thresh, gocv.MorphClose, kernel)

	// Find contours
	contours := gocv.FindContours(thresh, gocv.RetrievalExternal, gocv.ChainApproxSimple)
	defer contours.Close()

	for i := 0; i < contours.Size(); i++ {
		contour := contours.At(i)
		boundingRect := gocv.BoundingRect(contour)
		area := boundingRect.Dx() * boundingRect.Dy()
		aspectRatio := float64(boundingRect.Dx()) / float64(boundingRect.Dy())

		if area >= sys.minPlateArea && area <= sys.maxPlateArea &&
			aspectRatio >= sys.minAspectRatio && aspectRatio <= sys.maxAspectRatio {

			candidate := PlateCandidate{
				Contour:     contour,
				BoundingBox: boundingRect,
				AspectRatio: aspectRatio,
				Area:        area,
				Confidence:  sys.calculateRegionConfidence(gray, boundingRect) * 0.9,
			}
			candidates = append(candidates, candidate)
		}
	}

	return candidates
}

// removeDuplicateCandidates removes overlapping candidates
func (sys *PlateRecognitionSystem) removeDuplicateCandidates(candidates []PlateCandidate) []PlateCandidate {
	if len(candidates) <= 1 {
		return candidates
	}

	var unique []PlateCandidate
	for i, c1 := range candidates {
		isDuplicate := false
		for j, c2 := range candidates {
			if i != j && sys.rectsOverlap(c1.BoundingBox, c2.BoundingBox, 0.5) {
				if c2.Confidence > c1.Confidence {
					isDuplicate = true
					break
				}
			}
		}
		if !isDuplicate {
			unique = append(unique, c1)
		}
	}
	return unique
}

// rectsOverlap checks if two rectangles overlap by a certain threshold
func (sys *PlateRecognitionSystem) rectsOverlap(r1, r2 image.Rectangle, threshold float64) bool {
	intersection := r1.Intersect(r2)
	if intersection.Empty() {
		return false
	}

	area1 := float64(r1.Dx() * r1.Dy())
	area2 := float64(r2.Dx() * r2.Dy())
	intersectionArea := float64(intersection.Dx() * intersection.Dy())

	return intersectionArea/area1 > threshold || intersectionArea/area2 > threshold
}

// calculateRegionConfidence estimates how likely a region contains a license plate
func (sys *PlateRecognitionSystem) calculateRegionConfidence(gray gocv.Mat, rect image.Rectangle) float64 {
	confidence := 0.0

	// Extract ROI
	roi := gray.Region(rect)
	defer roi.Close()

	// Check for horizontal text-like patterns
	thresh := gocv.NewMat()
	defer thresh.Close()
	gocv.Threshold(roi, &thresh, 0, 255, gocv.ThresholdBinary+gocv.ThresholdOtsu)

	width := thresh.Cols()
	height := thresh.Rows()

	if height > 0 && width > 0 {
		// Check multiple rows for consistency
		transitions := 0
		for y := height / 4; y < 3*height/4; y += height / 8 {
			rowTransitions := 0
			lastPixel := thresh.GetUCharAt(y, 0)
			for x := 1; x < width; x++ {
				currentPixel := thresh.GetUCharAt(y, x)
				if currentPixel != lastPixel {
					rowTransitions++
				}
				lastPixel = currentPixel
			}
			transitions += rowTransitions
		}

		avgTransitions := float64(transitions) / 4.0
		confidence += avgTransitions / float64(width) * 100

		// Aspect ratio bonus
		aspectRatio := float64(rect.Dx()) / float64(rect.Dy())
		if aspectRatio >= 2.5 && aspectRatio <= 4.5 {
			confidence += 25
		} else if aspectRatio >= 2.0 && aspectRatio <= 5.0 {
			confidence += 15
		}

		// Size bonus
		area := rect.Dx() * rect.Dy()
		if area >= 8000 && area <= 30000 {
			confidence += 20
		} else if area >= 5000 && area <= 40000 {
			confidence += 10
		}

		// Position bonus (plates are usually in the middle-lower part)
		centerY := rect.Min.Y + rect.Dy()/2
		imageHeight := gray.Rows()
		if float64(centerY) > float64(imageHeight)*0.3 && float64(centerY) < float64(imageHeight)*0.8 {
			confidence += 10
		}
	}

	return math.Min(confidence, 100)
}

// performOCR executes OCR on a specific region with multiple preprocessing attempts
func (sys *PlateRecognitionSystem) performOCR(img gocv.Mat, region image.Rectangle) (string, float64) {
	if sys.ocrClient == nil {
		return sys.performSimpleOCR(img, region)
	}

	// Extract ROI
	roi := img.Region(region)
	defer roi.Close()

	bestText := ""
	bestConfidence := 0.0

	// Try multiple preprocessing techniques
	preprocessMethods := []func(gocv.Mat) gocv.Mat{
		sys.preprocessForOCR,
		sys.preprocessSimple,
		sys.preprocessInverted,
	}

	for _, preprocess := range preprocessMethods {
		processed := preprocess(roi)
		defer processed.Close()

		// Convert Mat to bytes for Tesseract
		buf, err := gocv.IMEncode(".png", processed)
		if err != nil {
			continue
		}

		// Perform OCR
		err = sys.ocrClient.SetImageFromBytes(buf.GetBytes())
		buf.Close()
		if err != nil {
			continue
		}

		text, err := sys.ocrClient.Text()
		if err != nil {
			continue
		}

		// Clean and evaluate
		text = strings.TrimSpace(text)
		confidence := sys.estimateTextConfidence(text)

		// Keep the best result
		if confidence > bestConfidence {
			bestText = text
			bestConfidence = confidence
		}

		// If we found a valid plate with high confidence, stop trying
		normalizedText := sys.normalizeText(text)
		if valid, _ := sys.isValidPlate(normalizedText); valid && confidence > 80 {
			break
		}
	}

	return bestText, bestConfidence
}

// preprocessSimple applies simple preprocessing
func (sys *PlateRecognitionSystem) preprocessSimple(roi gocv.Mat) gocv.Mat {
	processed := gocv.NewMat()

	// Resize
	resized := gocv.NewMat()
	defer resized.Close()
	gocv.Resize(roi, &resized, image.Pt(0, 0), 2.5, 2.5, gocv.InterpolationCubic)

	// Convert to grayscale
	gray := gocv.NewMat()
	defer gray.Close()
	if resized.Channels() > 1 {
		gocv.CvtColor(resized, &gray, gocv.ColorBGRToGray)
	} else {
		resized.CopyTo(&gray)
	}

	// Simple threshold
	gocv.Threshold(gray, &processed, 0, 255, gocv.ThresholdBinary+gocv.ThresholdOtsu)
	return processed
}

// preprocessInverted applies inverted preprocessing (for dark plates)
func (sys *PlateRecognitionSystem) preprocessInverted(roi gocv.Mat) gocv.Mat {
	processed := gocv.NewMat()

	// Resize
	resized := gocv.NewMat()
	defer resized.Close()
	gocv.Resize(roi, &resized, image.Pt(0, 0), 3.0, 3.0, gocv.InterpolationCubic)

	// Convert to grayscale
	gray := gocv.NewMat()
	defer gray.Close()
	if resized.Channels() > 1 {
		gocv.CvtColor(resized, &gray, gocv.ColorBGRToGray)
	} else {
		resized.CopyTo(&gray)
	}

	// Invert colors
	inverted := gocv.NewMat()
	defer inverted.Close()
	gocv.BitwiseNot(gray, &inverted)

	// Apply threshold
	gocv.AdaptiveThreshold(inverted, &processed, 255, gocv.AdaptiveThresholdMean, gocv.ThresholdBinary, 15, 2)
	return processed
}

// estimateTextConfidence provides a confidence estimation based on text characteristics
func (sys *PlateRecognitionSystem) estimateTextConfidence(text string) float64 {
	if len(text) == 0 {
		return 0
	}

	confidence := 25.0 // Base confidence

	// Clean text for analysis
	cleanText := sys.normalizeText(text)

	// Length bonus (Brazilian plates are 7 characters)
	if len(cleanText) == 7 {
		confidence += 40
	} else if len(cleanText) >= 6 && len(cleanText) <= 8 {
		confidence += 25
	} else if len(cleanText) < 4 || len(cleanText) > 10 {
		return 0 // Too short or too long
	}

	// Check character composition
	letters := 0
	numbers := 0
	for i, char := range cleanText {
		if char >= 'A' && char <= 'Z' {
			letters++
			// First 3 should be letters
			if i < 3 {
				confidence += 3
			}
		} else if char >= '0' && char <= '9' {
			numbers++
			// Position 3-6 should have numbers
			if i >= 3 && i < 7 {
				confidence += 2
			}
		}
	}

	// Brazilian plates must have both letters and numbers
	if letters >= 3 && numbers >= 3 {
		confidence += 20
	} else if letters == 0 || numbers == 0 {
		confidence -= 30
	}

	// Pattern matching bonus
	if valid, format := sys.isValidPlate(cleanText); valid {
		confidence += 30
		if format == "Old Brazilian format" || format == "Mercosul format" {
			confidence += 10
		}
	}

	// Check for common OCR patterns that indicate good reading
	if len(cleanText) >= 7 {
		// Check if first 3 are letters
		firstThreeLetters := true
		for i := 0; i < 3 && i < len(cleanText); i++ {
			if cleanText[i] < 'A' || cleanText[i] > 'Z' {
				firstThreeLetters = false
				break
			}
		}
		if firstThreeLetters {
			confidence += 10
		}
	}

	return math.Max(math.Min(confidence, 100), 0)
}

// performSimpleOCR alternative OCR approach
func (sys *PlateRecognitionSystem) performSimpleOCR(img gocv.Mat, region image.Rectangle) (string, float64) {
	roi := img.Region(region)
	defer roi.Close()

	gray := gocv.NewMat()
	defer gray.Close()
	if roi.Channels() > 1 {
		gocv.CvtColor(roi, &gray, gocv.ColorBGRToGray)
	} else {
		roi.CopyTo(&gray)
	}

	thresh := gocv.NewMat()
	defer thresh.Close()
	gocv.Threshold(gray, &thresh, 0, 255, gocv.ThresholdBinary+gocv.ThresholdOtsu)

	confidence := sys.analyzeTextPattern(thresh)
	if confidence > 40 {
		return "DETECTED", confidence
	}
	return "", 0
}

// analyzeTextPattern provides basic text pattern analysis
func (sys *PlateRecognitionSystem) analyzeTextPattern(img gocv.Mat) float64 {
	if img.Empty() {
		return 0
	}

	height := img.Rows()
	width := img.Cols()
	if height == 0 || width == 0 {
		return 0
	}

	// Analyze multiple features
	confidence := 0.0

	// 1. Horizontal line segments
	midY := height / 2
	lineSegments := 0
	inSegment := false

	for x := 0; x < width; x++ {
		pixel := img.GetUCharAt(midY, x)
		if pixel > 128 {
			if !inSegment {
				lineSegments++
				inSegment = true
			}
		} else {
			inSegment = false
		}
	}

	// Expected 7-10 segments for a plate
	if lineSegments >= 5 && lineSegments <= 15 {
		confidence += float64(lineSegments) * 5
	}

	// 2. Vertical consistency
	verticalConsistency := 0
	for x := width / 4; x < 3*width/4; x += width / 8 {
		whitePixels := 0
		for y := height / 4; y < 3*height/4; y++ {
			if img.GetUCharAt(y, x) > 128 {
				whitePixels++
			}
		}
		if whitePixels > height/8 && whitePixels < 3*height/4 {
			verticalConsistency++
		}
	}

	confidence += float64(verticalConsistency) * 10

	return math.Min(confidence, 100)
}

// stabilizePlateReading uses temporal filtering to stabilize readings
func (sys *PlateRecognitionSystem) stabilizePlateReading(newPlate string) string {
	if newPlate == "" || newPlate == "SCANNING..." {
		return sys.stabilizedPlate
	}

	// Add to history
	sys.frameHistory = append(sys.frameHistory, newPlate)
	if len(sys.frameHistory) > sys.historySize {
		sys.frameHistory = sys.frameHistory[1:]
	}

	// Count occurrences
	plateCounts := make(map[string]int)
	for _, plate := range sys.frameHistory {
		plateCounts[plate]++
	}

	// Find most common plate
	maxCount := 0
	mostCommon := ""
	for plate, count := range plateCounts {
		if count > maxCount {
			maxCount = count
			mostCommon = plate
		}
	}

	// Update stabilized plate if we have enough consensus
	if maxCount >= sys.historySize/3 {
		sys.stabilizedPlate = mostCommon
	}

	return sys.stabilizedPlate
}

// detectPlates finds and recognizes license plates in the image
func (sys *PlateRecognitionSystem) detectPlates(img gocv.Mat) []PlateDetection {
	var detections []PlateDetection

	// Find potential plate regions
	candidates := sys.findPlateRegions(img)

	// Process each candidate region with OCR
	for _, candidate := range candidates {
		// Expand the bounding box slightly for better OCR
		expanded := sys.expandRect(candidate.BoundingBox, img.Cols(), img.Rows(), 1.1)

		text, confidence := sys.performOCR(img, expanded)

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

// expandRect expands a rectangle by a factor while keeping it within image bounds
func (sys *PlateRecognitionSystem) expandRect(rect image.Rectangle, maxWidth, maxHeight int, factor float64) image.Rectangle {
	width := rect.Dx()
	height := rect.Dy()

	expandX := int(float64(width) * (factor - 1) / 2)
	expandY := int(float64(height) * (factor - 1) / 2)

	newRect := image.Rect(
		max(0, rect.Min.X-expandX),
		max(0, rect.Min.Y-expandY),
		min(maxWidth, rect.Max.X+expandX),
		min(maxHeight, rect.Max.Y+expandY),
	)

	return newRect
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// verifyPlate checks for license plates in the image
func (sys *PlateRecognitionSystem) verifyPlate(img gocv.Mat) (bool, string) {
	detections := sys.detectPlates(img)

	for _, detection := range detections {
		// Log all detections for debugging
		normalizedText := sys.normalizeText(detection.Text)

		if len(normalizedText) >= 5 {
			fmt.Printf("Detecção: '%s' -> '%s' (Confiança: %.1f%%)\n",
				detection.Text, normalizedText, detection.Confidence)
		}

		// Skip if too short after normalization
		if len(normalizedText) < 6 {
			continue
		}

		// Validate plate format
		isValid, format := sys.isValidPlate(normalizedText)
		if !isValid {
			// Try partial matching for almost valid plates
			if len(normalizedText) == 7 {
				// Check if it's close to a valid format
				letters := 0
				numbers := 0
				for i, ch := range normalizedText {
					if i < 3 && ch >= 'A' && ch <= 'Z' {
						letters++
					} else if i >= 3 && ch >= '0' && ch <= '9' {
						numbers++
					}
				}

				// If it's mostly correct, consider it valid
				if letters >= 2 && numbers >= 3 {
					isValid = true
					format = "Partial match"
				}
			}

			if !isValid {
				continue
			}
		}

		fmt.Printf("✓ Placa válida detectada: %s (%s)\n", normalizedText, format)

		// Use temporal stabilization
		stabilizedPlate := sys.stabilizePlateReading(normalizedText)

		// Check if it's in the allowed list
		for _, allowedPlate := range sys.allowedPlates {
			if stabilizedPlate == strings.ToUpper(allowedPlate) {
				return true, stabilizedPlate
			}
		}

		// Return the stabilized plate even if not allowed
		if stabilizedPlate != "" {
			return false, stabilizedPlate
		}
	}

	// No valid plate found
	return false, "SCANNING..."
}

// drawDetections draws bounding boxes around detected regions
func (sys *PlateRecognitionSystem) drawDetections(img *gocv.Mat, debugImg *gocv.Mat) {
	if !sys.debugMode {
		return
	}

	// Find candidates for visualization
	candidates := sys.findPlateRegions(*img)

	// Create debug image with preprocessing stages
	if debugImg != nil && !debugImg.Empty() {
		debugParts := make([]gocv.Mat, 0)

		// Original grayscale
		gray := gocv.NewMat()
		gocv.CvtColor(*img, &gray, gocv.ColorBGRToGray)
		gocv.Resize(gray, &gray, image.Pt(320, 240), 0, 0, gocv.InterpolationLinear)
		debugParts = append(debugParts, gray)

		// Edge detection
		edges := gocv.NewMat()
		gocv.Canny(gray, &edges, 50, 150)
		debugParts = append(debugParts, edges)

		// Best candidate preprocessing
		if len(candidates) > 0 {
			roi := img.Region(candidates[0].BoundingBox)
			processed := sys.preprocessForOCR(roi)
			gocv.Resize(processed, &processed, image.Pt(320, 100), 0, 0, gocv.InterpolationLinear)
			debugParts = append(debugParts, processed)
			roi.Close()
		}

		// Combine debug images
		if len(debugParts) > 0 {
			// Concatenate vertically
			if len(debugParts) == 1 {
				debugParts[0].CopyTo(debugImg)
			} else if len(debugParts) == 2 {
				gocv.Vconcat(debugParts[0], debugParts[1], debugImg)
			} else if len(debugParts) >= 3 {
				temp := gocv.NewMat()
				gocv.Vconcat(debugParts[0], debugParts[1], &temp)
				gocv.Vconcat(temp, debugParts[2], debugImg)
				temp.Close()
			}
		}

		// Clean up
		for _, mat := range debugParts {
			mat.Close()
		}
	}

	// Draw bounding boxes on main image
	for i, candidate := range candidates {
		if i >= 5 { // Limit visual clutter
			break
		}

		color := BLUE
		thickness := 2
		if i == 0 { // Best candidate
			color = YELLOW
			thickness = 3
		} else if i == 1 { // Second best
			color = GREEN
		}

		gocv.Rectangle(img, candidate.BoundingBox, color, thickness)

		// Draw confidence and info
		info := fmt.Sprintf("%.0f%% AR:%.1f", candidate.Confidence, candidate.AspectRatio)
		textPt := image.Pt(candidate.BoundingBox.Min.X, candidate.BoundingBox.Min.Y-5)

		// Background for text
		textSize := gocv.GetTextSize(info, gocv.FontHersheySimplex, 0.5, 1)
		gocv.Rectangle(img,
			image.Rect(textPt.X-2, textPt.Y-textSize.Y-2, textPt.X+textSize.X+2, textPt.Y+2),
			BLACK, -1)

		gocv.PutText(img, info, textPt, gocv.FontHersheySimplex, 0.5, color, 1)
	}
}

// drawInterface draws the main interface
func (sys *PlateRecognitionSystem) drawInterface(img *gocv.Mat) {
	height := img.Rows()

	// Draw semi-transparent background at bottom
	overlay := gocv.NewMat()
	img.CopyTo(&overlay)
	gocv.Rectangle(&overlay, image.Rect(0, height-180, 900, height), BLACK, -1)
	gocv.AddWeighted(*img, 0.7, overlay, 0.3, 0, img)
	overlay.Close()

	// Title
	gocv.PutText(img, "SISTEMA DE RECONHECIMENTO DE PLACAS",
		image.Pt(20, height-150), gocv.FontHersheySimplex, 0.7, WHITE, 2)

	// Allowed plates
	platesStr := strings.Join(sys.allowedPlates, ", ")
	gocv.PutText(img, fmt.Sprintf("Placas Autorizadas: %s", platesStr),
		image.Pt(20, height-120), gocv.FontHersheySimplex, 0.6, WHITE, 1)

	// Detected plate with larger font
	detectedColor := RED
	statusIcon := "❌"
	if sys.accessGranted {
		detectedColor = GREEN
		statusIcon = "✅"
	}

	plateText := fmt.Sprintf("%s Placa: %s", statusIcon, sys.detectedPlate)
	gocv.PutText(img, plateText,
		image.Pt(20, height-85), gocv.FontHersheySimplex, 0.8, detectedColor, 2)

	// Access status
	status := "ACESSO NEGADO"
	if sys.accessGranted {
		status = "ACESSO LIBERADO"
	}
	gocv.PutText(img, status,
		image.Pt(20, height-50), gocv.FontHersheySimplex, 1.0, detectedColor, 3)

	// Technical info
	ocrStatus := "Tesseract OCR"
	if sys.ocrClient == nil {
		ocrStatus = "Modo Simples"
	}

	info := fmt.Sprintf("%s | Min.Conf: %.0f%% | Frames: %d | Última: %s",
		ocrStatus, sys.minConfidence, len(sys.frameHistory),
		sys.lastVerification.Format("15:04:05"))

	gocv.PutText(img, info,
		image.Pt(20, height-20), gocv.FontHersheySimplex, 0.4, WHITE, 1)

	// Instructions
	gocv.PutText(img, "Q=Sair | D=Debug | Posicione a placa no centro da tela",
		image.Pt(20, 30), gocv.FontHersheySimplex, 0.5, WHITE, 1)
}

// Run executes the main system loop
func (sys *PlateRecognitionSystem) Run() error {
	img := gocv.NewMat()
	defer img.Close()

	debugImg := gocv.NewMat()
	defer debugImg.Close()

	fmt.Println("\n=== SISTEMA DE RECONHECIMENTO DE PLACAS VEICULARES ===")
	fmt.Println("Versão Otimizada com Múltiplas Técnicas de Detecção")
	fmt.Println("====================================================")
	fmt.Printf("Placas autorizadas: %s\n", strings.Join(sys.allowedPlates, ", "))
	fmt.Printf("Confiança mínima: %.0f%%\n", sys.minConfidence)
	fmt.Printf("Estabilização: %d frames\n", sys.historySize)

	if sys.ocrClient != nil {
		fmt.Println("✓ Tesseract OCR: Ativo")
	} else {
		fmt.Println("✗ Tesseract OCR: Indisponível (modo simples)")
	}

	fmt.Println("\nComandos:")
	fmt.Println("  Q - Sair")
	fmt.Println("  D - Alternar modo debug")
	fmt.Println("\nDicas para melhor detecção:")
	fmt.Println("  • Posicione a placa no centro da imagem")
	fmt.Println("  • Mantenha boa iluminação")
	fmt.Println("  • Evite reflexos na placa")
	fmt.Println("  • Mantenha a câmera estável")
	fmt.Println("====================================================\n")

	frameCount := 0
	startTime := time.Now()

	for {
		// Capture frame
		if ok := sys.camera.Read(&img); !ok {
			fmt.Println("Erro ao capturar frame da câmera")
			break
		}

		if img.Empty() {
			continue
		}

		frameCount++

		// Flip horizontally for mirror effect
		gocv.Flip(img, &img, 1)

		// Check for new plate detection
		if time.Since(sys.lastVerification) > sys.waitTime {
			sys.accessGranted, sys.detectedPlate = sys.verifyPlate(img)
			sys.lastVerification = time.Now()

			if sys.accessGranted {
				fmt.Printf("✅ [%s] ACESSO LIBERADO - Placa: %s\n",
					time.Now().Format("15:04:05"), sys.detectedPlate)
			} else if sys.detectedPlate != "SCANNING..." && sys.detectedPlate != "" {
				fmt.Printf("❌ [%s] ACESSO NEGADO - Placa: %s (não autorizada)\n",
					time.Now().Format("15:04:05"), sys.detectedPlate)
			}
		}

		// Draw detection boxes and debug info
		sys.drawDetections(&img, &debugImg)

		// Draw interface
		sys.drawInterface(&img)

		// Calculate and display FPS
		if frameCount%30 == 0 {
			elapsed := time.Since(startTime).Seconds()
			fps := float64(frameCount) / elapsed
			gocv.PutText(&img, fmt.Sprintf("FPS: %.1f", fps),
				image.Pt(img.Cols()-100, 30), gocv.FontHersheySimplex, 0.5, GREEN, 1)
		}

		// Show main window
		sys.window.IMShow(img)

		// Show debug window if enabled
		if sys.debugMode && sys.debugWindow != nil && !debugImg.Empty() {
			sys.debugWindow.IMShow(debugImg)
		}

		// Handle keyboard input
		key := sys.window.WaitKey(1)
		switch key {
		case 'q', 'Q', 27: // Quit (q, Q, or ESC)
			fmt.Println("\nEncerrando sistema...")
			return nil
		case 'd', 'D': // Toggle debug mode
			sys.debugMode = !sys.debugMode
			if sys.debugMode {
				fmt.Println("🔍 Modo debug: ATIVADO")
				if sys.debugWindow == nil {
					sys.debugWindow = gocv.NewWindow("Debug - Plate Detection")
					sys.debugWindow.ResizeWindow(800, 600)
				}
			} else {
				fmt.Println("🔍 Modo debug: DESATIVADO")
				if sys.debugWindow != nil {
					sys.debugWindow.Close()
					sys.debugWindow = nil
				}
			}
		case 'r', 'R': // Reset history
			sys.frameHistory = make([]string, 0)
			sys.stabilizedPlate = ""
			fmt.Println("🔄 Histórico resetado")
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

	fmt.Println("\nSistema encerrado com sucesso.")
}
