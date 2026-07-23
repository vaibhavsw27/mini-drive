package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/redis/go-redis/v9"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var redisClient *redis.Client
var minioClient *minio.Client
var dbConn *pgx.Conn

var (
	requestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"path", "method", "status"},
	)

	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "Duration of HTTP requests in seconds",
		},
		[]string{"path"},
	)
)

func init() {
	prometheus.MustRegister(requestCount)
	prometheus.MustRegister(requestDuration)
}

var jwtSecret = []byte("super-secret-key-change-this-later")

type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type ShareRequest struct {
	Filename       string `json:"filename"`
	ShareWithEmail string `json:"share_with_email"`
}

type contextKey string

const userIDKey contextKey = "user_id"

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

func startChecksumWorker() {
	go func() {
		ctx := context.Background()
		for {
			result, err := redisClient.BRPop(ctx, 0, "checksum_jobs").Result()
			if err != nil {
				fmt.Println("Worker error:", err)
				continue
			}

			jobData := result[1]
			parts := strings.SplitN(jobData, "|", 2)
			if len(parts) != 2 {
				continue
			}
			storageKey := parts[0]
			filename := parts[1]

			object, err := minioClient.GetObject(ctx, "mini-drive-files", storageKey, minio.GetObjectOptions{})
			if err != nil {
				fmt.Println("Worker: failed to fetch file:", err)
				continue
			}

			hasher := sha256.New()
			_, err = io.Copy(hasher, object)
			object.Close()
			if err != nil {
				fmt.Println("Worker: failed to hash file:", err)
				continue
			}

			checksum := hex.EncodeToString(hasher.Sum(nil))

			_, err = dbConn.Exec(ctx,
				"UPDATE files SET checksum=$1 WHERE storage_key=$2", checksum, storageKey)
			if err != nil {
				fmt.Println("Worker: failed to save checksum:", err)
				continue
			}

			fmt.Printf("Worker: computed checksum for %s: %s\n", filename, checksum)
		}
	}()
}

func signupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SignupRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	_, err = dbConn.Exec(context.Background(),
		"INSERT INTO users (email, password_hash) VALUES ($1, $2)",
		req.Email, string(hashedPassword))
	if err != nil {
		http.Error(w, "failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintln(w, "user created successfully")
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var userID int
	var storedHash string
	err = dbConn.QueryRow(context.Background(),
		"SELECT id, password_hash FROM users WHERE email=$1", req.Email).Scan(&userID, &storedHash)
	if err != nil {
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(req.Password))
	if err != nil {
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}

	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": tokenString})
}

func shareHandler(w http.ResponseWriter, r *http.Request) {
	ownerID := r.Context().Value(userIDKey)

	var req ShareRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	var fileID int
	err = dbConn.QueryRow(ctx,
		"SELECT id FROM files WHERE owner_id=$1 AND filename=$2", ownerID, req.Filename).Scan(&fileID)
	if err != nil {
		http.Error(w, "file not found or you don't own it", http.StatusNotFound)
		return
	}

	var shareWithUserID int
	err = dbConn.QueryRow(ctx,
		"SELECT id FROM users WHERE email=$1", req.ShareWithEmail).Scan(&shareWithUserID)
	if err != nil {
		http.Error(w, "user to share with not found", http.StatusNotFound)
		return
	}

	_, err = dbConn.Exec(ctx,
		"INSERT INTO file_shares (file_id, shared_with_user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		fileID, shareWithUserID)
	if err != nil {
		http.Error(w, "failed to share file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "File '%s' shared with %s\n", req.Filename, req.ShareWithEmail)
}

func metricsMiddleware(path string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next(rec, r)

		duration := time.Since(start).Seconds()
		requestDuration.WithLabelValues(path).Observe(duration)
		requestCount.WithLabelValues(path, r.Method, fmt.Sprintf("%d", rec.status)).Inc()
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// authMiddleware checks for a valid JWT token before allowing the request through
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "invalid token claims", http.StatusUnauthorized)
			return
		}

		userID := claims["user_id"]
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next(w, r.WithContext(ctx))
	}
}

// meHandler is a protected test route - only accessible with a valid token
func meHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey)
	fmt.Fprintf(w, "You are logged in as user_id: %v\n", userID)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey)

	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "failed to get file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ctx := context.Background()

	var latestVersion int
	err = dbConn.QueryRow(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM files WHERE owner_id=$1 AND filename=$2",
		userID, header.Filename).Scan(&latestVersion)
	if err != nil {
		http.Error(w, "failed to check existing versions", http.StatusInternalServerError)
		return
	}

	newVersion := latestVersion + 1
	storageKey := fmt.Sprintf("%v_%s_v%d", userID, header.Filename, newVersion)

	_, err = minioClient.PutObject(ctx, "mini-drive-files", storageKey, file, header.Size, minio.PutObjectOptions{})
	if err != nil {
		http.Error(w, "failed to upload to storage: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = dbConn.Exec(ctx,
		"INSERT INTO files (owner_id, filename, storage_key, size, version) VALUES ($1, $2, $3, $4, $5)",
		userID, header.Filename, storageKey, header.Size, newVersion)
	if err != nil {
		http.Error(w, "failed to save file metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jobData := fmt.Sprintf("%s|%s", storageKey, header.Filename)
	err = redisClient.LPush(ctx, "checksum_jobs", jobData).Err()
	if err != nil {
		fmt.Println("Failed to queue checksum job:", err)
	}

	fmt.Fprintf(w, "File uploaded successfully: %s (version %d)\n", header.Filename, newVersion)
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(userIDKey)
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		http.Error(w, "filename query param required", http.StatusBadRequest)
		return
	}

	versionParam := r.URL.Query().Get("version")
	ctx := context.Background()

	cacheKey := fmt.Sprintf("filekey:%v:%s:%s", userID, filename, versionParam)

	var storageKey string

	cached, err := redisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		storageKey = cached
		fmt.Println("Cache hit for", cacheKey)
	} else {
		fmt.Println("Cache miss for", cacheKey)

		if versionParam == "" {
			err = dbConn.QueryRow(ctx, `
				SELECT f.storage_key FROM files f
				WHERE f.filename=$2 AND (
					f.owner_id=$1
					OR f.id IN (SELECT file_id FROM file_shares WHERE shared_with_user_id=$1)
				)
				ORDER BY f.version DESC LIMIT 1`, userID, filename).Scan(&storageKey)
		} else {
			err = dbConn.QueryRow(ctx, `
				SELECT f.storage_key FROM files f
				WHERE f.filename=$2 AND f.version=$3 AND (
					f.owner_id=$1
					OR f.id IN (SELECT file_id FROM file_shares WHERE shared_with_user_id=$1)
				)`, userID, filename, versionParam).Scan(&storageKey)
		}

		if err != nil {
			http.Error(w, "file not found or access denied", http.StatusNotFound)
			return
		}

		redisClient.Set(ctx, cacheKey, storageKey, 5*time.Minute)
	}

	object, err := minioClient.GetObject(ctx, "mini-drive-files", storageKey, minio.GetObjectOptions{})
	if err != nil {
		http.Error(w, "failed to get file from storage", http.StatusInternalServerError)
		return
	}
	defer object.Close()

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(filename))
	_, err = io.Copy(w, object)
	if err != nil {
		http.Error(w, "failed to send file", http.StatusInternalServerError)
		return
	}
}

func connectRedis() {
	redisClient = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		fmt.Println("Unable to connect to Redis:", err)
		return
	}
	fmt.Println("Connected to Redis successfully!")
}

func connectMinio() {
	client, err := minio.New("localhost:9000", &minio.Options{
		Creds:  credentials.NewStaticV4("minidrive", "minidrive123", ""),
		Secure: false,
	})
	if err != nil {
		fmt.Println("Unable to connect to MinIO:", err)
		return
	}
	minioClient = client

	ctx := context.Background()
	bucketName := "mini-drive-files"
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		fmt.Println("Error checking bucket:", err)
		return
	}
	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			fmt.Println("Error creating bucket:", err)
			return
		}
	}
	fmt.Println("Connected to MinIO successfully!")
}

func connectDB() {
	connString := "postgres://minidrive:minidrive123@localhost:5433/minidrive_db"

	conn, err := pgx.Connect(context.Background(), connString)
	if err != nil {
		fmt.Println("Unable to connect to database:", err)
		return
	}
	dbConn = conn
	fmt.Println("Connected to Postgres successfully!")
}

func main() {
	connectDB()
	connectMinio()
	connectRedis()
	startChecksumWorker()

	http.HandleFunc("/health", metricsMiddleware("/health", healthHandler))
	http.HandleFunc("/signup", metricsMiddleware("/signup", signupHandler))
	http.HandleFunc("/login", metricsMiddleware("/login", loginHandler))
	http.HandleFunc("/me", metricsMiddleware("/me", authMiddleware(meHandler)))
	http.HandleFunc("/upload", metricsMiddleware("/upload", authMiddleware(uploadHandler)))
	http.HandleFunc("/download", metricsMiddleware("/download", authMiddleware(downloadHandler)))
	http.HandleFunc("/share", metricsMiddleware("/share", authMiddleware(shareHandler)))
	http.Handle("/metrics", promhttp.Handler())

	fmt.Println("Server starting on port 8080...")
	http.ListenAndServe(":8080", nil)
}
