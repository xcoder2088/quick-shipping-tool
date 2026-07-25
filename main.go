package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxUploadSize = 50 << 20 // 50 MB
	maxPhotos     = 10

	mailerSendURL = "https://api.mailersend.com/v1/email"
)

// ==================================================
// API RESPONSE TO OUR PWA
// ==================================================

type UploadResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	Customer    string `json:"customer,omitempty"`
	Shipment    string `json:"shipment,omitempty"`
	Destination string `json:"destination,omitempty"`
	UserEmail   string `json:"user_email,omitempty"`
	Photos      int    `json:"photos,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
}

// ==================================================
// MAILERSEND STRUCTURES
// ==================================================

type MailerSendAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type MailerSendAttachment struct {
	Content     string `json:"content"`
	Filename    string `json:"filename"`
	Disposition string `json:"disposition,omitempty"`
}

type MailerSendRequest struct {
	From        MailerSendAddress      `json:"from"`
	To          []MailerSendAddress    `json:"to"`
	ReplyTo     MailerSendAddress      `json:"reply_to"`
	Subject     string                 `json:"subject"`
	Text        string                 `json:"text"`
	HTML        string                 `json:"html,omitempty"`
	Attachments []MailerSendAttachment `json:"attachments,omitempty"`
}

// ==================================================
// MAIN
// ==================================================

func main() {

	// ==================================================
	// STATIC FILES
	// ==================================================

	fs := http.FileServer(
		http.Dir("./static"),
	)

	http.Handle(
		"/static/",
		http.StripPrefix(
			"/static/",
			fs,
		),
	)

	// ==================================================
	// ROUTES
	// ==================================================

	http.HandleFunc(
		"/",
		homeHandler,
	)

	http.HandleFunc(
		"/api/upload",
		uploadHandler,
	)

	// ==================================================
	// CHECK MAILERSEND CONFIGURATION
	// ==================================================

	apiKey := strings.TrimSpace(
		os.Getenv("MAILERSEND_API_KEY"),
	)

	fromEmail := strings.TrimSpace(
		os.Getenv("MAILERSEND_FROM"),
	)

	fmt.Println("=================================")
	fmt.Println("  Quick Shipping Tool")
	fmt.Println("  http://localhost:8081")

	if apiKey != "" && fromEmail != "" {
		fmt.Println("  MailerSend: READY")
	} else {
		fmt.Println("  MailerSend: NOT CONFIGURED")
	}

	fmt.Println("=================================")

	// ==================================================
	// SERVER
	// ==================================================

	log.Fatal(
		http.ListenAndServe(
			":8081",
			nil,
		),
	)
}

// ==================================================
// HOME
// ==================================================

func homeHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	if r.URL.Path != "/" {

		http.NotFound(
			w,
			r,
		)

		return
	}

	http.ServeFile(
		w,
		r,
		"./templates/index.html",
	)
}

// ==================================================
// UPLOAD
// ==================================================

func uploadHandler(
	w http.ResponseWriter,
	r *http.Request,
) {

	// ==================================================
	// POST ONLY
	// ==================================================

	if r.Method != http.MethodPost {

		writeJSON(
			w,
			http.StatusMethodNotAllowed,
			UploadResponse{
				Success: false,
				Message: "Method not allowed",
			},
		)

		return
	}

	// ==================================================
	// MAILERSEND CONFIGURATION
	// ==================================================

	apiKey := strings.TrimSpace(
		os.Getenv("MAILERSEND_API_KEY"),
	)

	fromEmail := strings.TrimSpace(
		os.Getenv("MAILERSEND_FROM"),
	)

	if apiKey == "" {

		log.Println(
			"MAILERSEND_API_KEY is not configured",
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Email service is not configured",
			},
		)

		return
	}

	if fromEmail == "" {

		log.Println(
			"MAILERSEND_FROM is not configured",
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Sender email is not configured",
			},
		)

		return
	}

	if !validEmail(fromEmail) {

		log.Println(
			"MAILERSEND_FROM contains an invalid email address",
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Sender email configuration is invalid",
			},
		)

		return
	}

	// ==================================================
	// UPLOAD SIZE LIMIT
	// ==================================================

	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		maxUploadSize,
	)

	if err := r.ParseMultipartForm(
		maxUploadSize,
	); err != nil {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Unable to process upload. The files may be too large.",
			},
		)

		return
	}

	// ==================================================
	// CUSTOMER
	// ==================================================

	customer := strings.TrimSpace(
		r.FormValue("customer"),
	)

	if customer == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Customer Name is required",
			},
		)

		return
	}

	safeCustomer := sanitizeValue(
		customer,
	)

	if safeCustomer == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Invalid Customer Name",
			},
		)

		return
	}

	// ==================================================
	// SHIPMENT / PO
	// ==================================================

	shipmentOriginal := strings.TrimSpace(
		r.FormValue("shipment"),
	)

	if shipmentOriginal == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Shipment / PO is required",
			},
		)

		return
	}

	shipment := sanitizeValue(
		shipmentOriginal,
	)

	if shipment == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Invalid Shipment / PO",
			},
		)

		return
	}

	// ==================================================
	// DESTINATION EMAIL
	// ==================================================

	destination := strings.TrimSpace(
		r.FormValue("destination"),
	)

	if destination == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Destination email is required",
			},
		)

		return
	}

	if !validEmail(destination) {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Invalid destination email",
			},
		)

		return
	}

	// ==================================================
	// USER EMAIL
	// ==================================================

	userEmail := strings.TrimSpace(
		r.FormValue("user_email"),
	)

	if userEmail == "" {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "User email is required",
			},
		)

		return
	}

	if !validEmail(userEmail) {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Invalid user email",
			},
		)

		return
	}

	// ==================================================
	// PHOTOS
	// ==================================================

	files := r.MultipartForm.File["photos"]

	if len(files) == 0 {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "At least one photo is required",
			},
		)

		return
	}

	if len(files) > maxPhotos {

		writeJSON(
			w,
			http.StatusBadRequest,
			UploadResponse{
				Success: false,
				Message: "Maximum 10 photos per shipment",
			},
		)

		return
	}

	// ==================================================
	// CREATE UPLOAD DIRECTORY
	// ==================================================

	uploadDir := filepath.Join(
		"uploads",
		shipment,
	)

	if err := os.MkdirAll(
		uploadDir,
		0755,
	); err != nil {

		log.Println(
			"Unable to create upload directory:",
			err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Unable to create shipment directory",
			},
		)

		return
	}

	// ==================================================
	// TIMESTAMP
	// ==================================================

	now := time.Now()

	timestamp := now.Format(
		"20060102_150405",
	)

	// ==================================================
	// SAVE PHOTOS
	// ==================================================

	saved := 0

	savedFiles := make(
		[]string,
		0,
		len(files),
	)

	for i, header := range files {

		// --------------------------------------------------
		// OPEN UPLOADED FILE
		// --------------------------------------------------

		src, err := header.Open()

		if err != nil {

			log.Println(
				"Unable to open photo:",
				err,
			)

			continue
		}

		// --------------------------------------------------
		// DETERMINE EXTENSION
		// --------------------------------------------------

		extension := strings.ToLower(
			filepath.Ext(
				header.Filename,
			),
		)

		if extension == "" {

			contentType := header.Header.Get(
				"Content-Type",
			)

			extensions, _ := mime.ExtensionsByType(
				contentType,
			)

			if len(extensions) > 0 {

				extension = extensions[0]

			} else {

				extension = ".jpg"
			}
		}

		// --------------------------------------------------
		// ALLOW COMMON IMAGE EXTENSIONS ONLY
		// --------------------------------------------------

		if !allowedImageExtension(
			extension,
		) {

			src.Close()

			log.Printf(
				"Rejected unsupported file: %s",
				header.Filename,
			)

			continue
		}

		// --------------------------------------------------
		// GENERATE FILENAME
		// --------------------------------------------------

		filename := fmt.Sprintf(
			"%s_%s_%s_%02d%s",
			safeCustomer,
			shipment,
			timestamp,
			i+1,
			extension,
		)

		destinationPath := filepath.Join(
			uploadDir,
			filename,
		)

		// --------------------------------------------------
		// CREATE DESTINATION FILE
		// --------------------------------------------------

		dst, err := os.Create(
			destinationPath,
		)

		if err != nil {

			src.Close()

			log.Println(
				"Unable to create photo:",
				err,
			)

			continue
		}

		// --------------------------------------------------
		// COPY PHOTO
		// --------------------------------------------------

		_, copyErr := io.Copy(
			dst,
			src,
		)

		dst.Close()
		src.Close()

		if copyErr != nil {

			os.Remove(
				destinationPath,
			)

			log.Println(
				"Unable to save photo:",
				copyErr,
			)

			continue
		}

		saved++

		savedFiles = append(
			savedFiles,
			destinationPath,
		)

		log.Printf(
			"Customer: %s | Shipment: %s | Saved: %s",
			customer,
			shipmentOriginal,
			destinationPath,
		)
	}

	// ==================================================
	// VERIFY PHOTOS
	// ==================================================

	if saved == 0 {

		writeJSON(
			w,
			http.StatusInternalServerError,
			UploadResponse{
				Success: false,
				Message: "Unable to save photos",
			},
		)

		return
	}

	// ==================================================
	// PREPARE EMAIL
	// ==================================================

	emailSubject := fmt.Sprintf(
		"%s - Shipment %s",
		customer,
		shipmentOriginal,
	)

	emailBody := fmt.Sprintf(
		"Quick Shipping Tool\n\n"+
			"Customer: %s\n"+
			"Shipment / PO: %s\n"+
			"Submitted by: %s\n"+
			"Date: %s\n"+
			"Photos: %d\n",
		customer,
		shipmentOriginal,
		userEmail,
		now.Format("2006-01-02 15:04:05"),
		saved,
	)

	emailHTML := fmt.Sprintf(
		`<!DOCTYPE html>
<html>
<body style="font-family: Arial, sans-serif; color: #1f2937;">
	<h2>Quick Shipping Tool</h2>

	<p>
		<strong>Customer:</strong> %s<br>
		<strong>Shipment / PO:</strong> %s<br>
		<strong>Submitted by:</strong> %s<br>
		<strong>Date:</strong> %s<br>
		<strong>Photos:</strong> %d
	</p>
</body>
</html>`,
		htmlEscape(customer),
		htmlEscape(shipmentOriginal),
		htmlEscape(userEmail),
		htmlEscape(now.Format("2006-01-02 15:04:05")),
		saved,
	)

	// ==================================================
	// LOG EMAIL INFORMATION
	// ==================================================

	log.Println(
		"========================================",
	)

	log.Println(
		"EMAIL READY",
	)

	log.Println(
		"TO:",
		destination,
	)

	log.Println(
		"FROM:",
		fromEmail,
	)

	log.Println(
		"REPLY-TO:",
		userEmail,
	)

	log.Println(
		"SUBJECT:",
		emailSubject,
	)

	for _, file := range savedFiles {

		log.Println(
			"ATTACHMENT:",
			file,
		)
	}

	log.Println(
		"========================================",
	)

	// ==================================================
	// SEND EMAIL THROUGH MAILERSEND
	// ==================================================

	messageID, err := sendMailerSendEmail(
		apiKey,
		fromEmail,
		destination,
		userEmail,
		emailSubject,
		emailBody,
		emailHTML,
		savedFiles,
	)

	if err != nil {

		log.Println(
			"MAILERSEND ERROR:",
			err,
		)

		log.Println(
			"Photos were NOT deleted because email sending failed.",
		)

		writeJSON(
			w,
			http.StatusBadGateway,
			UploadResponse{
				Success:     false,
				Message:     "Unable to send email: " + err.Error(),
				Customer:    customer,
				Shipment:    shipmentOriginal,
				Destination: destination,
				UserEmail:   userEmail,
				Photos:      saved,
			},
		)

		return
	}

	// ==================================================
	// MAILERSEND ACCEPTED EMAIL
	// ==================================================

	log.Println(
		"========================================",
	)

	log.Println(
		"MAILERSEND ACCEPTED EMAIL",
	)

	if messageID != "" {

		log.Println(
			"MESSAGE ID:",
			messageID,
		)
	}

	log.Println(
		"Deleting temporary photos...",
	)

	// ==================================================
	// DELETE PHOTOS IMMEDIATELY
	// ==================================================

	deleteErrors := false

	for _, file := range savedFiles {

		err := os.Remove(
			file,
		)

		if err != nil {

			deleteErrors = true

			log.Println(
				"Unable to delete temporary photo:",
				file,
				err,
			)

			continue
		}

		log.Println(
			"DELETED:",
			file,
		)
	}

	// ==================================================
	// REMOVE SHIPMENT DIRECTORY IF EMPTY
	// ==================================================

	err = os.Remove(
		uploadDir,
	)

	if err == nil {

		log.Println(
			"REMOVED EMPTY DIRECTORY:",
			uploadDir,
		)

	} else if !os.IsNotExist(err) {

		log.Println(
			"Shipment directory was not removed:",
			err,
		)
	}

	log.Println(
		"========================================",
	)

	// ==================================================
	// SUCCESS RESPONSE
	// ==================================================

	message := "Photos sent successfully"

	if deleteErrors {

		message = "Photos sent successfully, but some temporary files could not be deleted"
	}

	writeJSON(
		w,
		http.StatusOK,
		UploadResponse{
			Success:     true,
			Message:     message,
			Customer:    customer,
			Shipment:    shipmentOriginal,
			Destination: destination,
			UserEmail:   userEmail,
			Photos:      saved,
			MessageID:   messageID,
		},
	)
}

// ==================================================
// SEND EMAIL THROUGH MAILERSEND API
// ==================================================

func sendMailerSendEmail(
	apiKey string,
	fromEmail string,
	destination string,
	replyTo string,
	subject string,
	textBody string,
	htmlBody string,
	files []string,
) (string, error) {

	// ==================================================
	// PREPARE ATTACHMENTS
	// ==================================================

	attachments := make(
		[]MailerSendAttachment,
		0,
		len(files),
	)

	for _, filePath := range files {

		data, err := os.ReadFile(
			filePath,
		)

		if err != nil {

			return "",
				fmt.Errorf(
					"unable to read attachment %s: %w",
					filepath.Base(filePath),
					err,
				)
		}

		encoded := base64.StdEncoding.EncodeToString(
			data,
		)

		attachments = append(
			attachments,
			MailerSendAttachment{
				Content:     encoded,
				Filename:    filepath.Base(filePath),
				Disposition: "attachment",
			},
		)
	}

	// ==================================================
	// BUILD MAILERSEND REQUEST
	// ==================================================

	payload := MailerSendRequest{
		From: MailerSendAddress{
			Email: fromEmail,
			Name:  "Quick Shipping Tool",
		},

		To: []MailerSendAddress{
			{
				Email: destination,
			},
		},

		ReplyTo: MailerSendAddress{
			Email: replyTo,
		},

		Subject: subject,

		Text: textBody,

		HTML: htmlBody,

		Attachments: attachments,
	}

	// ==================================================
	// JSON
	// ==================================================

	jsonData, err := json.Marshal(
		payload,
	)

	if err != nil {

		return "",
			fmt.Errorf(
				"unable to create MailerSend request: %w",
				err,
			)
	}

	// ==================================================
	// HTTP REQUEST
	// ==================================================

	req, err := http.NewRequest(
		http.MethodPost,
		mailerSendURL,
		bytes.NewReader(jsonData),
	)

	if err != nil {

		return "",
			fmt.Errorf(
				"unable to create MailerSend HTTP request: %w",
				err,
			)
	}

	req.Header.Set(
		"Authorization",
		"Bearer "+apiKey,
	)

	req.Header.Set(
		"Content-Type",
		"application/json",
	)

	req.Header.Set(
		"Accept",
		"application/json",
	)

	// ==================================================
	// HTTP CLIENT
	// ==================================================

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	response, err := client.Do(
		req,
	)

	if err != nil {

		return "",
			fmt.Errorf(
				"unable to contact MailerSend: %w",
				err,
			)
	}

	defer response.Body.Close()

	// ==================================================
	// READ RESPONSE
	// ==================================================

	responseBody, err := io.ReadAll(
		io.LimitReader(
			response.Body,
			1<<20,
		),
	)

	if err != nil {

		return "",
			fmt.Errorf(
				"unable to read MailerSend response: %w",
				err,
			)
	}

	// ==================================================
	// MAILERSEND MUST RETURN 202 ACCEPTED
	// ==================================================

	if response.StatusCode != http.StatusAccepted {

		errorMessage := strings.TrimSpace(
			string(responseBody),
		)

		if errorMessage == "" {

			errorMessage = response.Status
		}

		return "",
			fmt.Errorf(
				"MailerSend returned HTTP %d: %s",
				response.StatusCode,
				errorMessage,
			)
	}

	// ==================================================
	// MESSAGE ID
	// ==================================================

	messageID := strings.TrimSpace(
		response.Header.Get(
			"X-Message-Id",
		),
	)

	return messageID, nil
}

// ==================================================
// EMAIL VALIDATION
// ==================================================

func validEmail(
	value string,
) bool {

	address, err := mail.ParseAddress(
		value,
	)

	if err != nil {
		return false
	}

	return strings.EqualFold(
		address.Address,
		value,
	)
}

// ==================================================
// ALLOWED IMAGE EXTENSIONS
// ==================================================

func allowedImageExtension(
	extension string,
) bool {

	switch strings.ToLower(extension) {

	case ".jpg",
		".jpeg",
		".png",
		".heic",
		".heif",
		".webp":

		return true

	default:

		return false
	}
}

// ==================================================
// SANITIZE FILE / DIRECTORY VALUE
// ==================================================

func sanitizeValue(
	value string,
) string {

	value = strings.TrimSpace(
		value,
	)

	re := regexp.MustCompile(
		`[^a-zA-Z0-9_-]+`,
	)

	value = re.ReplaceAllString(
		value,
		"_",
	)

	return strings.Trim(
		value,
		"_",
	)
}

// ==================================================
// BASIC HTML ESCAPE
// ==================================================

func htmlEscape(
	value string,
) string {

	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)

	return replacer.Replace(
		value,
	)
}

// ==================================================
// JSON RESPONSE
// ==================================================

func writeJSON(
	w http.ResponseWriter,
	status int,
	response UploadResponse,
) {

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(
		status,
	)

	if err := json.NewEncoder(w).Encode(
		response,
	); err != nil {

		log.Println(
			"JSON response error:",
			err,
		)
	}
}
