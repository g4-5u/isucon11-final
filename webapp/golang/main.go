package main

import (
	"database/sql"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pborman/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	SQLDirectory           = "../sql/"
	AssignmentsDirectory   = "../assignments/"
	AssignmentTmpDirectory = "../assignments/tmp/"
	DocDirectory           = "../documents/"
	SessionName            = "session"
)

type handlers struct {
	DB *sqlx.DB
}

func main() {
	e := echo.New()
	e.Debug = GetEnv("DEBUG", "") != ""
	e.Server.Addr = fmt.Sprintf(":%v", GetEnv("PORT", "7000"))
	e.HideBanner = true

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(session.Middleware(sessions.NewCookieStore([]byte("trapnomura"))))

	db, _ := GetDB(false)

	h := &handlers{
		DB: db,
	}

	// e.POST("/initialize", h.Initialize, h.IsLoggedIn, h.IsAdmin)
	e.POST("/initialize", h.Initialize)
	e.PUT("/phase", h.SetPhase, h.IsLoggedIn, h.IsAdmin)

	e.POST("/login", h.Login)
	API := e.Group("/api", h.IsLoggedIn)
	{
		usersAPI := API.Group("/users")
		{
			usersAPI.GET("/me/courses", h.GetRegisteredCourses)
			usersAPI.PUT("/me/courses", h.RegisterCourses)
			usersAPI.GET("/me/grades", h.GetGrades)
		}
		syllabusAPI := API.Group("/syllabus")
		{
			syllabusAPI.GET("", h.SearchCourses)
			syllabusAPI.GET("/:courseID", h.GetCourseDetail)
		}
		coursesAPI := API.Group("/courses")
		{
			coursesAPI.GET("/:courseID", h.GetCourseDetail)
			coursesAPI.GET("/:courseID/classes", h.GetClasses)
			coursesAPI.GET("/:courseID/documents", h.GetDocumentList)
			coursesAPI.POST("/:courseID/classes/:classID/documents", h.PostDocumentFile, h.IsAdmin)
			coursesAPI.GET("/:courseID/documents/:documentID", h.DownloadDocumentFile)
			coursesAPI.GET("/:courseID/assignments", h.GetAssignmentList)
			coursesAPI.POST("/:courseID/classes/:classID/assignments", h.PostAssignment, h.IsAdmin)
			coursesAPI.POST("/:courseID/assignments/:assignmentID", h.SubmitAssignment)
			coursesAPI.GET("/:courseID/assignments/:assignmentID/export", h.DownloadSubmittedAssignment, h.IsAdmin)
			coursesAPI.GET("/:courseID/classes/:classID/code", h.GetAttendanceCode, h.IsAdmin)
			coursesAPI.POST("/:courseID/classes", h.AddClass, h.IsAdmin)
			coursesAPI.POST("/:courseID/classes/:classID", h.SetClassFlag, h.IsAdmin)
			coursesAPI.GET("/:courseID/classes/:classID/attendances", h.GetAttendances, h.IsAdmin)
			coursesAPI.POST("/:courseID/announcements", h.AddAnnouncements, h.IsAdmin)
			coursesAPI.POST("/:courseID/grades", h.SetUserGrades, h.IsAdmin)
		}
		announcementsAPI := API.Group("/announcements")
		{
			announcementsAPI.GET("", h.GetAnnouncementList)
			announcementsAPI.GET("/:announcementID", h.GetAnnouncementDetail)
		}
		attendanceCodeAPI := API.Group("/attendance_codes")
		{
			attendanceCodeAPI.POST("", h.PostAttendanceCode)
		}
	}

	e.Logger.Error(e.StartServer(e.Server))
}

type InitializeResponse struct {
	Language string `json:"language"`
}

func (h *handlers) Initialize(c echo.Context) error {
	dbForInit, _ := GetDB(true)

	files := []string{
		"schema.sql",
		"test_data.sql",
	}
	for _, file := range files {
		data, err := ioutil.ReadFile(SQLDirectory + file)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("read sql file: %v", err))
		}
		if _, err := dbForInit.Exec(string(data)); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("exec sql file: %v", err))
		}
	}

	res := InitializeResponse{
		Language: "go",
	}
	return c.JSON(http.StatusOK, res)
}

func (h *handlers) IsLoggedIn(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		sess, err := session.Get(SessionName, c)
		if err != nil {
			return echo.ErrInternalServerError
		}
		if sess.IsNew {
			return echo.NewHTTPError(http.StatusUnauthorized, "You are not logged in.")
		}
		if _, ok := sess.Values["userID"]; !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "You are not logged in.")
		}

		return next(c)
	}
}

func (h *handlers) IsAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		sess, err := session.Get(SessionName, c)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("get session: %v", err))
		}
		isAdmin, ok := sess.Values["isAdmin"]
		if !ok {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("get session value: %v", err))
		}
		if !isAdmin.(bool) {
			return echo.NewHTTPError(http.StatusForbidden, "You are not admin user.")
		}

		return next(c)
	}
}

type SetPhaseRequest struct {
	Phase    PhaseType `json:"phase"`
	Year     uint32    `json:"year"`
	Semester Semester  `json:"semester"`
}

func (h *handlers) SetPhase(c echo.Context) error {
	var req SetPhaseRequest
	if err := c.Bind(&req); err != nil {
		log.Println(err)
		return echo.NewHTTPError(http.StatusBadRequest, err)
	}

	if req.Phase != PhaseRegistration && req.Phase != PhaseClass && req.Phase != PhaseResult {
		return echo.NewHTTPError(http.StatusBadRequest, "bad phase")
	}
	if req.Semester != FirstSemester && req.Semester != SecondSemester {
		return echo.NewHTTPError(http.StatusBadRequest, "bad semester")
	}

	if _, err := h.DB.Exec("TRUNCATE TABLE `phase`"); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}
	if _, err := h.DB.Exec("INSERT INTO `phase` (`phase`, `year`, `semester`) VALUES (?, ?, ?)", req.Phase, req.Year, req.Semester); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusNoContent)
}

type GetGradesResponse struct {
	Summary      Summary        `json:"summary"`
	CourseGrades []*CourseGrade `json:"courses"`
}

type PostGradesRequest []PostGradeRequest

type Summary struct {
	Credits int    `json:"credits"`
	GPT     uint32 `json:"gpt"`
}

type CourseGrade struct {
	ID     uuid.UUID `json:"id" db:"course_id"`
	Name   string    `json:"name" db:"name"`
	Credit uint8     `json:"credit" db:"credit"`
	Grade  string    `json:"grade" db:"grade"`
}

type PostGradeRequest struct {
	UserID uuid.UUID `json:"user_id"`
	Grade  string    `json:"grade"`
}

type GetAttendanceCodeResponse struct {
	Code string `json:"code"`
}

type GetAnnouncementsResponse []GetAnnouncementResponse
type GetAnnouncementResponse struct {
	ID         uuid.UUID `json:"id"`
	CourseName string    `json:"course_name"`
	Title      string    `json:"title"`
	// MEMO: TODO: 既読機能
	// Unread     bool      `json:"unread"`
	CreatedAt int64 `json:"created_at"`
}

type GetAnnouncementDetailResponse struct {
	ID         uuid.UUID `json:"id"`
	CourseName string    `json:"course_name"`
	Title      string    `json:"title"`
	Message    string    `json:"message"`
	CreatedAt  int64     `json:"created_at"`
}

type PostAnnouncementsRequest struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type PostAnnouncementsResponse struct {
	ID uuid.UUID `json:"id"`
}

type GetAttendancesAttendance struct {
	UserID     uuid.UUID `json:"user_id"`
	AttendedAt int64     `json:"attended_at"`
}

type GetAttendancesResponse []GetAttendancesAttendance

type PostAttendanceCodeRequest struct {
	Code string `json:"code"`
}

type PostDocumentResponse struct {
	ID uuid.UUID `json:"id"`
}

type TimeSlotResponse struct {
	Period    uint8  `json:"period"`
	DayOfWeek string `json:"day_of_week"`
}

type PhaseType string

const (
	PhaseRegistration PhaseType = "reg"
	PhaseClass        PhaseType = "class"
	PhaseResult       PhaseType = "result"
)

type Semester string

const (
	FirstSemester  Semester = "first"
	SecondSemester Semester = "second"
)

type Phase struct {
	Phase    PhaseType `json:"phase"`
	Year     uint32    `json:"year"`
	Semester Semester  `json:"semester"`
}

type UserType string

const (
	_       UserType = "student" /* FIXME: use Student */
	Faculty UserType = "faculty"
)

type User struct {
	ID             uuid.UUID `db:"id"`
	Code           string    `db:"code"`
	Name           string    `db:"name"`
	HashedPassword []byte    `db:"hashed_password"`
	Type           UserType  `db:"type"`
}

type Course struct {
	ID          uuid.UUID    `db:"id"`
	Code        string       `db:"code"`
	Type        string       `db:"type"`
	Name        string       `db:"name"`
	Description string       `db:"description"`
	Credit      uint8        `db:"credit"`
	Period      uint8        `db:"period"`
	DayOfWeek   string       `db:"day_of_week"`
	TeacherID   uuid.UUID    `db:"teacher_id"`
	Keywords    string       `db:"keywords"`
	Status      CourseStatus `db:"status"`
	CreatedAt   time.Time    `db:"created_at"`
}

type CourseStatus string

const (
	StatusReg    CourseStatus = "reg"
	_            CourseStatus = "class" /* FIXME: use StatusClass */
	StatusResult CourseStatus = "result"
)

type Schedule struct {
	ID        uuid.UUID `db:"id"`
	Period    uint8     `db:"period"`
	DayOfWeek string    `db:"day_of_week"`
	Semester  Semester  `db:"semester"`
	Year      uint32    `db:"year"`
}

type Class struct {
	ID             uuid.UUID `db:"id"`
	CourseID       uuid.UUID `db:"course_id"`
	Part           uint8     `db:"part"`
	Title          string    `db:"title"`
	Description    string    `db:"description"`
	AttendanceCode string    `db:"attendance_code"`
}

type Attendance struct {
	ClassID   uuid.UUID `db:"class_id"`
	UserID    uuid.UUID `db:"user_id"`
	CreatedAt time.Time `db:"created_at"`
}

type DocumentsMeta struct {
	ID        uuid.UUID `db:"id"`
	ClassID   uuid.UUID `db:"class_id"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

type Assignment struct {
	ID          uuid.UUID `db:"id"`
	ClassID     uuid.UUID `db:"class_id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	CreatedAt   time.Time `db:"created_at"`
}

type Announcement struct {
	ID         uuid.UUID `db:"id"`
	CourseName string    `db:"name"`
	Title      string    `db:"title"`
	Message    string    `db:"message"`
	CreatedAt  time.Time `db:"created_at"`
}

type SubmissionWithUserName struct {
	ID           uuid.UUID `db:"id"`
	UserID       uuid.UUID `db:"user_id"`
	UserName     string    `db:"user_name"`
	AssignmentID uuid.UUID `db:"assignment_id"`
	Name         string    `db:"name"`
	CreatedAt    time.Time `db:"created_at"`
}

type LoginRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

func (h *handlers) Login(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		c.Logger().Error(err)
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("bind request: %v", err))
	}

	var user User
	err := h.DB.Get(&user, "SELECT * FROM `users` WHERE `code` = ?", req.Code)
	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusUnauthorized, "Code or Password is wrong.")
	} else if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	if bcrypt.CompareHashAndPassword(user.HashedPassword, []byte(req.Password)) != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Code or Password is wrong.")
	}

	sess, err := session.Get(SessionName, c)
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	if s, ok := sess.Values["userID"].(string); ok {
		userID := uuid.Parse(s)
		if uuid.Equal(userID, user.ID) {
			return echo.NewHTTPError(http.StatusBadRequest, "You are already logged in.")
		}
	}

	sess.Values["userID"] = user.ID.String()
	sess.Values["userName"] = user.Name
	sess.Values["isAdmin"] = user.Type == Faculty
	sess.Options = &sessions.Options{
		Path:   "/",
		MaxAge: 3600,
	}

	if err := sess.Save(c.Request(), c.Response()); err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}

type GetRegisteredCourseResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Teacher   string    `json:"teacher"`
	Period    uint8     `json:"period"`
	DayOfWeek string    `json:"day_of_week"`
}

func (h *handlers) GetRegisteredCourses(c echo.Context) error {
	sess, err := session.Get(SessionName, c)
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}
	userID := uuid.Parse(sess.Values["userID"].(string))
	if userID == nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	var courses []Course
	if err = h.DB.Select(&courses, "SELECT `courses`.* "+
		"FROM `courses` "+
		"JOIN `registrations` ON `courses`.`id` = `registrations`.`course_id` "+
		"WHERE `courses`.`status` != ? AND `registrations`.`user_id` = ? AND `registrations`.`deleted_at` IS NULL", StatusResult, userID); err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	res := make([]GetRegisteredCourseResponse, 0, len(courses))
	for _, course := range courses {
		var teacher User
		if err := h.DB.Get(&teacher, "SELECT * FROM `users` WHERE `id` = ?", course.TeacherID); err != nil {
			c.Logger().Error(err)
			return c.NoContent(http.StatusInternalServerError)
		}

		res = append(res, GetRegisteredCourseResponse{
			ID:        course.ID,
			Name:      course.Name,
			Teacher:   teacher.Name,
			Period:    course.Period,
			DayOfWeek: course.DayOfWeek,
		})
	}

	return c.JSON(http.StatusOK, res)
}

type RegisterCourseRequest struct {
	ID string `json:"id"`
}

type RegisterCoursesRequest []RegisterCourseRequest

type RegisterCoursesErrorResponse struct {
	NotFoundCourse        []string    `json:"not_found_course,omitempty"`
	StatusNotRegistration []uuid.UUID `json:"status_not_registration,omitempty"`
	TimeslotDuplicated    []uuid.UUID `json:"timeslot_duplicated,omitempty"`
}

func (h *handlers) RegisterCourses(c echo.Context) error {
	sess, err := session.Get(SessionName, c)
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}
	userID := uuid.Parse(sess.Values["userID"].(string))
	if userID == nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	var req RegisterCoursesRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("bind request: %v", err))
	}

	tx, err := h.DB.Beginx()
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	hasError := false
	var errors RegisterCoursesErrorResponse
	var courses []Course
	for _, courseReq := range req {
		courseID := uuid.Parse(courseReq.ID)
		if courseID == nil {
			hasError = true
			errors.NotFoundCourse = append(errors.NotFoundCourse, courseReq.ID)
			continue
		}

		var course Course
		if err := tx.Get(&course, "SELECT * FROM `courses` WHERE `id` = ? FOR SHARE", courseID); err == sql.ErrNoRows {
			hasError = true
			errors.NotFoundCourse = append(errors.NotFoundCourse, courseReq.ID)
			continue
		} else if err != nil {
			_ = tx.Rollback()
			c.Logger().Error(err)
			return c.NoContent(http.StatusInternalServerError)
		}

		if course.Status != StatusReg {
			hasError = true
			errors.StatusNotRegistration = append(errors.StatusNotRegistration, course.ID)
			continue
		}

		// MEMO: すでに履修登録済みの科目は無視する
		var registerCount int
		if err := tx.Get(&registerCount, "SELECT COUNT(*) FROM `registrations` WHERE `course_id` = ? AND `user_id` = ?", course.ID, userID); err != nil {
			_ = tx.Rollback()
			c.Logger().Error(err)
			return c.NoContent(http.StatusInternalServerError)
		}
		if registerCount > 0 {
			continue
		}

		courses = append(courses, course)
	}

	if len(courses) > 0 {
		// MEMO: スケジュールの重複バリデーション
		var registeredCourses []Course
		if err := tx.Select(&registeredCourses, "SELECT `courses`.* "+
			"FROM `courses` "+
			"JOIN `registrations` ON `courses`.`id` = `registrations`.`course_id` "+
			"WHERE `courses`.`status` != ? AND `registrations`.`user_id` = ? AND `registrations`.`deleted_at` IS NULL", StatusResult, userID); err != nil {
			_ = tx.Rollback()
			c.Logger().Error(err)
			return c.NoContent(http.StatusInternalServerError)
		}

		registeredCourses = append(registeredCourses, courses...)

		for _, course1 := range courses {
			for _, course2 := range registeredCourses {
				if !uuid.Equal(course1.ID, course2.ID) && course1.Period == course2.Period && course1.DayOfWeek == course2.DayOfWeek {
					hasError = true
					errors.TimeslotDuplicated = append(errors.TimeslotDuplicated, course1.ID)
					break
				}
			}
		}
	}

	if hasError {
		_ = tx.Rollback()
		return c.JSON(http.StatusBadRequest, errors)
	}

	for _, course := range courses {
		_, err = tx.Exec("INSERT INTO `registrations` (`course_id`, `user_id`, `created_at`) VALUES (?, ?, NOW(6))", course.ID, userID)
		if err != nil {
			_ = tx.Rollback()
			log.Println(err)
			return c.NoContent(http.StatusInternalServerError)
		}
	}

	if err = tx.Commit(); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}

func (h *handlers) GetGrades(context echo.Context) error {
	sess, err := session.Get(SessionName, context)
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	userID := uuid.Parse(sess.Values["userID"].(string))
	if userID == nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	userIDParam := uuid.Parse(context.Param("userID"))
	if userIDParam == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid userID")
	}
	if !uuid.Equal(userID, userIDParam) {
		return echo.NewHTTPError(http.StatusForbidden, "invalid userID")
	}

	// MEMO: GradeテーブルとCoursesテーブルから、対象userIDのcourse_id/name/credit/gradeを取得
	var CourseGrades []CourseGrade
	query := "SELECT `course_id`, `name`, `credit`, `grade`" +
		"FROM `grades`" +
		"JOIN `courses` ON `grades`.`course_id` = `courses`.`id`" +
		"WHERE `user_id` = ?"
	if err := h.DB.Select(&CourseGrades, query, userID); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	var res GetGradesResponse
	var grade uint32
	var gpt uint32 = 0

	var credits int = 0
	if len(CourseGrades) > 0 {
		for _, coursegrade := range CourseGrades {
			res.CourseGrades = append(res.CourseGrades, &CourseGrade{
				ID:     coursegrade.ID,
				Name:   coursegrade.Name,
				Credit: coursegrade.Credit,
				Grade:  coursegrade.Grade,
			})

			switch coursegrade.Grade {
			case "S":
				grade = 4
			case "A":
				grade = 3
			case "B":
				grade = 2
			case "C":
				grade = 1
			case "D":
				grade = 0
			}
			credits += int(coursegrade.Credit)
			gpt += grade * uint32(coursegrade.Credit)
		}
	}

	res.Summary = Summary{
		Credits: credits,
		GPT:     gpt,
	}

	return context.JSON(http.StatusOK, res)
}

func (h *handlers) SearchCourses(context echo.Context) error {
	panic("implement me")
}

type GetCourseDetailResponse struct {
	ID              uuid.UUID                `json:"id"`
	Code            string                   `json:"code"`
	Type            string                   `json:"type"`
	Name            string                   `json:"name"`
	Description     string                   `json:"description"`
	Credit          uint8                    `json:"credit"`
	Classroom       string                   `json:"classroom"`
	Capacity        uint32                   `json:"capacity,omitempty"`
	Teacher         string                   `json:"teacher"`
	Keywords        string                   `json:"keywords"`
	Semester        Semester                 `json:"semester"`
	Year            uint32                   `json:"year"`
	Timeslots       []TimeSlotResponse       `json:"timeslots"`
	RequiredCourses []RequiredCourseResponse `json:"required_courses"`
}

type RequiredCourseResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

func (h *handlers) GetCourseDetail(context echo.Context) error {
	courseID := uuid.Parse(context.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}

	var course Course
	if err := h.DB.Get(&course, "SELECT * FROM `courses` WHERE `id` = ?", courseID); err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "No such course")
	} else if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	var teacher User
	if err := h.DB.Get(&teacher, "SELECT * FROM `users` WHERE `id` = ?", course.TeacherID); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	var schedules []Schedule
	if err := h.DB.Select(&schedules, "SELECT `schedules`.* "+
		"FROM `schedules` "+
		"JOIN `course_schedules` ON `schedules`.`id` = `course_schedules`.`schedule_id` "+
		"WHERE `course_schedules`.`course_id` = ?", course.ID); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	if len(schedules) == 0 {
		return context.NoContent(http.StatusInternalServerError)
	}

	var requiredCourses []Course
	if err := h.DB.Select(&requiredCourses, "SELECT `courses`.* "+
		"FROM `course_requirements` "+
		"JOIN `courses` ON `course_requirements`.`required_course_id` = `courses`.`id` "+
		"WHERE `course_requirements`.`course_id` = ?", course.ID); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	timeslotsRes := make([]TimeSlotResponse, 0, len(schedules))
	for _, schedule := range schedules {
		timeslotsRes = append(timeslotsRes, TimeSlotResponse{
			Period:    schedule.Period,
			DayOfWeek: schedule.DayOfWeek,
		})
	}

	requiredCoursesRes := make([]RequiredCourseResponse, 0, len(requiredCourses))
	for _, course := range requiredCourses {
		requiredCoursesRes = append(requiredCoursesRes, RequiredCourseResponse{
			ID:   course.ID,
			Name: course.Name,
		})
	}

	res := GetCourseDetailResponse{
		ID:              course.ID,
		Code:            course.Code,
		Type:            course.Type,
		Name:            course.Name,
		Description:     course.Description,
		Credit:          course.Credit,
		Classroom:       course.Classroom,
		Teacher:         teacher.Name,
		Keywords:        course.Keywords,
		Semester:        schedules[0].Semester,
		Year:            schedules[0].Year,
		Timeslots:       timeslotsRes,
		RequiredCourses: requiredCoursesRes,
	}

	if course.Capacity.Valid {
		res.Capacity = uint32(course.Capacity.Int32)
	}

	return context.JSON(http.StatusOK, res)
}

type GetClassResponse struct {
	ID          uuid.UUID `json:"id"`
	Part        uint8     `json:"part"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
}

func (h *handlers) GetClasses(c echo.Context) error {
	courseID := uuid.Parse(c.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}

	var course Course
	if err := h.DB.Get(&course, "SELECT * FROM `courses` WHERE `id` = ?", courseID); err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "course not found")
	} else if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	var classes []Class
	if err := h.DB.Select(&classes, "SELECT * FROM `classes` WHERE `course_id` = ? ORDER BY `part`", courseID); err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	res := make([]GetClassResponse, 0, len(classes))
	for _, class := range classes {
		res = append(res, GetClassResponse{
			ID:          class.ID,
			Part:        class.Part,
			Title:       class.Title,
			Description: class.Description,
		})
	}

	return c.JSON(http.StatusOK, res)
}

type PostAssignmentRequest struct {
	Name        string
	Description string
}

type PostAssignmentResponse struct {
	ID uuid.UUID `json:"id"`
}

func (h *handlers) PostAssignment(context echo.Context) error {
	var req PostAssignmentRequest
	if err := context.Bind(&req); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	if req.Name == "" || req.Description == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name and description must not be empty.")
	}

	classID := context.Param("classID")
	var classes int
	if err := h.DB.Get(&classes, "SELECT COUNT(*) FROM `classes` WHERE `id` = ?", classID); err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusBadRequest, "No such class.")
	} else if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	assignmentID := uuid.NewRandom()
	if _, err := h.DB.Exec("INSERT INTO `assignments` (`id`, `class_id`, `name`, `description`, `created_at`) VALUES (?, ?, ?, ?, NOW(6))", assignmentID, classID, req.Name, req.Description); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	return context.JSON(http.StatusCreated, PostAssignmentResponse{
		ID: assignmentID,
	})
}

type GetDocumentResponse struct {
	ID      uuid.UUID `json:"id"`
	ClassID uuid.UUID `json:"class_id"`
	Name    string    `json:"name"`
}

type GetDocumentsResponse []GetDocumentResponse

func (h *handlers) GetDocumentList(context echo.Context) error {
	courseID := uuid.Parse(context.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}

	documentsMeta := make([]DocumentsMeta, 0)
	err := h.DB.Select(&documentsMeta, "SELECT `documents`.* FROM `documents` JOIN `classes` ON `classes`.`id` = `documents`.`class_id` WHERE `classes`.`course_id` = ?", courseID)
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	res := make(GetDocumentsResponse, 0, len(documentsMeta))
	for _, meta := range documentsMeta {
		res = append(res, GetDocumentResponse{
			ID:      meta.ID,
			ClassID: meta.ClassID,
			Name:    meta.Name,
		})
	}

	return context.JSON(http.StatusOK, res)

}

func (h *handlers) PostDocumentFile(context echo.Context) error {
	courseID := uuid.Parse(context.Param("courseID"))
	if uuid.Equal(uuid.NIL, courseID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}
	classID := uuid.Parse(context.Param("classID"))
	if uuid.Equal(uuid.NIL, classID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid classID")
	}

	form, err := context.MultipartForm()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "read request err")
	}
	files := form.File["files"]

	// 作ったファイルの名前を格納しておく
	dsts := make([]string, 0, len(files))

	tx, err := h.DB.Begin()
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	// 作成したファイルを削除する
	deleteFiles := func(dsts []string) {
		for _, file := range dsts {
			os.Remove(file)
		}
	}

	res := make([]PostDocumentResponse, 0, len(files))
	for _, file := range files {
		src, err := file.Open()
		if err != nil {
			log.Println(err)
			_ = tx.Rollback()
			deleteFiles(dsts)
			return context.NoContent(http.StatusInternalServerError)
		}

		fileMeta := DocumentsMeta{
			ID:      uuid.NewRandom(),
			ClassID: classID,
			Name:    file.Filename,
		}

		filePath := fmt.Sprintf("%s%s", DocDirectory, fileMeta.ID)

		dst, err := os.Create(filePath)
		if err != nil {
			log.Println(err)
			_ = tx.Rollback()
			deleteFiles(dsts)
			return context.NoContent(http.StatusInternalServerError)
		}

		dsts = append(dsts, filePath)
		_, err = tx.Exec("INSERT INTO `documents` (`id`, `class_id`, `name`, `created_at`) VALUES (?, ?, ?, NOW(6))",
			fileMeta.ID,
			fileMeta.ClassID,
			fileMeta.Name,
		)
		if err != nil {
			log.Println(err)
			_ = tx.Rollback()
			deleteFiles(dsts)
			return context.NoContent(http.StatusInternalServerError)
		}

		if _, err = io.Copy(dst, src); err != nil {
			log.Println(err)
			_ = tx.Rollback()
			deleteFiles(dsts)
			return context.NoContent(http.StatusInternalServerError)
		}

		res = append(res, PostDocumentResponse{ID: fileMeta.ID})
	}

	err = tx.Commit()
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	return context.JSON(http.StatusCreated, res)
}

type GetAssignmentResponse struct {
	ID          uuid.UUID `json:"id"`
	ClassID     uuid.UUID `json:"class_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

type GetAssignmentsResponse []GetAssignmentResponse

func (h *handlers) GetAssignmentList(c echo.Context) error {
	courseID := uuid.Parse(c.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}

	var assignments []Assignment
	if err := h.DB.Select(&assignments, "SELECT `assignments`.* "+
		"FROM `assignments` "+
		"JOIN `classes` ON `assignments`.`class_id` = `classes`.`id` "+
		"WHERE `classes`.`course_id` = ?", courseID); err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	res := make(GetAssignmentsResponse, 0, len(assignments))
	for _, assignment := range assignments {
		res = append(res, GetAssignmentResponse{
			ID:          assignment.ID,
			ClassID:     assignment.ClassID,
			Name:        assignment.Name,
			Description: assignment.Description,
		})
	}

	return c.JSON(http.StatusOK, res)
}

func (h *handlers) DownloadDocumentFile(context echo.Context) error {
	courseID := uuid.Parse(context.Param("courseID"))
	if uuid.Equal(uuid.NIL, courseID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}
	documentID := uuid.Parse(context.Param("documentID"))
	if uuid.Equal(uuid.NIL, documentID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid classID")
	}

	var documentMeta DocumentsMeta
	err := h.DB.Get(&documentMeta, "SELECT `documents`.* FROM `documents` JOIN `classes` ON `classes`.`id` = `documents`.`class_id` "+
		"WHERE `documents`.`id` = ? AND `classes`.`course_id` = ?", documentID, courseID)
	if err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "file not found")
	} else if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	filePath := fmt.Sprintf("%s%s", DocDirectory, documentMeta.ID)
	return context.File(filePath)
}

func (h *handlers) SubmitAssignment(context echo.Context) error {
	sess, err := session.Get(SessionName, context)
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	userID := uuid.Parse(sess.Values["userID"].(string))
	if userID == nil {
		return context.NoContent(http.StatusInternalServerError)
	}

	courseID := uuid.Parse(context.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}

	if ok, err := h.courseIsInCurrentPhase(courseID); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	} else if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "The course is not started yet or has ended.")
	}

	assignmentID := context.Param("assignmentID")
	var assignments int
	if err := h.DB.Get(&assignments, "SELECT COUNT(*) FROM `assignments` WHERE `id` = ?", assignmentID); err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusBadRequest, "No such assignment.")
	} else if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	file, err := context.FormFile("file")
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	src, err := file.Open()
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	defer src.Close()

	submissionID := uuid.New()
	dst, err := os.Create(AssignmentsDirectory + submissionID)
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	defer dst.Close()

	if _, err = io.Copy(dst, src); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	if _, err := h.DB.Exec("INSERT INTO `submissions` (`id`, `user_id`, `assignment_id`, `name`, `created_at`) VALUES (?, ?, ?, ?, NOW(6))", submissionID, userID, assignmentID, file.Filename); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	return context.NoContent(http.StatusNoContent)
}

func (h *handlers) DownloadSubmittedAssignment(c echo.Context) error {
	courseID := uuid.Parse(c.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}

	assignmentID := uuid.Parse(c.Param("assignmentID"))
	if assignmentID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid assignmentID")
	}

	tx, err := h.DB.Beginx()
	if err != nil {
		c.Logger().Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	// MEMO: zipファイルを作るためFOR UPDATEでassignment、FOR SHAREでsubmissionをロック
	var assignment Assignment
	if err := tx.Get(&assignment, "SELECT * FROM `assignments` WHERE `id` = ? FOR UPDATE", assignmentID); err == sql.ErrNoRows {
		_ = tx.Rollback()
		return echo.NewHTTPError(http.StatusBadRequest, "No such assignment.")
	} else if err != nil {
		c.Logger().Error(err)
		_ = tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError)
	}
	var submissions []*SubmissionWithUserName
	if err := tx.Select(&submissions,
		"SELECT `submissions`.*, `users`.`name` AS `user_name` "+
			"FROM `submissions` JOIN `users` ON `users`.`id` = `submissions`.`user_id`"+
			"WHERE `assignment_id` = ? ORDER BY `user_id` FOR SHARE", assignmentID); err != nil && err != sql.ErrNoRows {
		c.Logger().Error(err)
		_ = tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	// MEMO: TODO: export時でなく提出時にzipファイルを作ることでボトルネックを作りたいが、「そうはならんやろ」という気持ち
	zipFilePath := AssignmentTmpDirectory + assignmentID.String() + ".zip"
	if err := createSubmissionsZip(zipFilePath, submissions); err != nil {
		c.Logger().Error(err)
		_ = tx.Rollback()
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	if err := tx.Commit(); err != nil {
		c.Logger().Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError)
	}

	return c.File(zipFilePath)
}

func (h *handlers) GetAttendanceCode(context echo.Context) error {
	courseID := uuid.Parse(context.Param("courseID"))
	if uuid.Equal(uuid.NIL, courseID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}
	classID := uuid.Parse(context.Param("classID"))
	if uuid.Equal(uuid.NIL, classID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid classID")
	}

	var res GetAttendanceCodeResponse
	if err := h.DB.Get(&res.Code, "SELECT `attendance_code` FROM `classes` WHERE `course_id` = ? AND `id` = ?", courseID, classID); err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "course or class not found")
	} else if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("get attendance code: %v", err))
	}

	return context.JSON(http.StatusOK, res)
}

type AddClassRequest struct {
	Part        uint8  `json:"part"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type AddClassResponse struct {
	ID             uuid.UUID `json:"id"`
	AttendanceCode string    `json:"attendance_code"`
}

func (h *handlers) AddClass(c echo.Context) error {
	courseID := uuid.Parse(c.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}

	var req AddClassRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("bind request: %v", err))
	}

	if req.Part == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid part")
	}

	classID := uuid.NewRandom()
	const lenCode = 6
	const mysqlDupEntryCode = 1062
	var attendanceCode string
	for {
		bytes := make([]byte, lenCode)
		for i := range bytes {
			bytes[i] = byte(65 + rand.Intn(26))
		}
		attendanceCode = string(bytes)

		if _, err := h.DB.Exec("INSERT INTO `classes` (`id`, `course_id`, `part`, `title`, `description`, `attendance_code`) VALUES (?, ?, ?, ?, ?, ?)",
			classID, courseID, req.Part, req.Title, req.Description, attendanceCode); err != nil {
			if mysqlErr, ok := err.(*mysql.MySQLError); ok && mysqlErr.Number == mysqlDupEntryCode {
				continue
			} else {
				c.Logger().Error(err)
				return c.NoContent(http.StatusInternalServerError)
			}
		}
		break
	}

	res := AddClassResponse{
		ID:             classID,
		AttendanceCode: attendanceCode,
	}

	return c.JSON(http.StatusCreated, res)
}

func (h *handlers) SetClassFlag(context echo.Context) error {
	panic("implement me")
}

func (h *handlers) GetAttendances(context echo.Context) error {
	courseID := uuid.Parse(context.Param("courseID"))
	if uuid.Equal(uuid.NIL, courseID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}
	classID := uuid.Parse(context.Param("classID"))
	if uuid.Equal(uuid.NIL, classID) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid classID")
	}

	var attendances []Attendance
	if err := h.DB.Select(&attendances, "SELECT * FROM `attendances` WHERE `class_id` = ?", classID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("get attendances: %v", err))
	}

	res := make(GetAttendancesResponse, len(attendances))
	for i, attendance := range attendances {
		res[i] = GetAttendancesAttendance{
			UserID:     attendance.UserID,
			AttendedAt: attendance.CreatedAt.UnixNano() / int64(time.Millisecond),
		}
	}

	return context.JSON(http.StatusOK, res)
}

func (h *handlers) AddAnnouncements(context echo.Context) error {
	courseID := uuid.Parse(context.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}
	var count int
	if err := h.DB.Get(&count, "SELECT COUNT(*) FROM `courses` WHERE `id` = ?", courseID); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	if count == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "No such course.")
	}

	var req PostAnnouncementsRequest
	if err := context.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("bind request: %v", err))
	}

	announcementID := uuid.NewRandom()
	if _, err := h.DB.Exec("INSERT INTO `announcements` (`id`, `course_id`, `title`, `message`, `created_at`) VALUES (?, ?, ?, ?, NOW(6))", announcementID, courseID, req.Title, req.Message); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	res := PostAnnouncementsResponse{
		ID: announcementID,
	}

	return context.JSON(http.StatusCreated, res)
}

func (h *handlers) SetUserGrades(context echo.Context) error {
	courseID := uuid.Parse(context.Param("courseID"))
	if courseID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid courseID")
	}

	var req PostGradesRequest
	if err := context.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("bind request: %v", err))
	}

	// MEMO: LOGIC: 学生一人の全コース成績登録
	for _, coursegrade := range req {
		if _, err := h.DB.Exec("INSERT INTO `grades` (`id`, `user_id`, `course_id`, `grade`) VALUES (?, ?, ?, ?)", uuid.New(), coursegrade.UserID, courseID, coursegrade.Grade); err != nil {
			log.Println(err)
			return context.NoContent(http.StatusInternalServerError)
		}
	}

	return context.NoContent(http.StatusNoContent)
}

func (h *handlers) GetAnnouncementList(context echo.Context) error {
	sess, err := session.Get(SessionName, context)
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	userID := uuid.Parse(sess.Values["userID"].(string))
	if userID == nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	// MEMO: ページングの初期実装はページ番号形式
	var page int
	if context.QueryParam("page") == "" {
		page = 1
	} else {
		page, err = strconv.Atoi(context.QueryParam("page"))
		if err != nil || page <= 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid page.")
		}
	}
	limit := 20
	offset := limit * (page - 1)

	announcements := make([]Announcement, 0)
	if err := h.DB.Select(&announcements, "SELECT `announcements`.`id`, `courses`.`name`, `announcements`.`title`, `announcements`.`message`, `announcements`.`created_at` "+
		"FROM `announcements` "+
		"JOIN `courses` ON `announcements`.`course_id` = `courses`.`id` "+
		"JOIN `registrations` ON `announcements`.`course_id` = `registrations`.`course_id` "+
		"WHERE `registrations`.`user_id` = ? AND `registrations`.`deleted_at` IS NULL "+
		"ORDER BY `announcements`.`created_at` DESC "+
		"LIMIT ? OFFSET ?", userID, limit+1, offset); err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	lenRes := len(announcements)
	if len(announcements) == limit+1 {
		lenRes = limit
	}
	res := make(GetAnnouncementsResponse, 0, lenRes)
	for _, announcement := range announcements[:lenRes] {
		res = append(res, GetAnnouncementResponse{
			ID:         announcement.ID,
			CourseName: announcement.CourseName,
			Title:      announcement.Title,
			CreatedAt:  announcement.CreatedAt.UnixNano() / int64(time.Millisecond),
		})
	}

	if lenRes > 0 {
		var links []string
		path := fmt.Sprintf("%v://%v%v", context.Scheme(), context.Request().Host, context.Path())
		if page > 1 {
			links = append(links, fmt.Sprintf("<%v?page=%v>; rel=\"prev\"", path, page-1))
		}
		if len(announcements) == limit+1 {
			links = append(links, fmt.Sprintf("<%v?page=%v>; rel=\"next\"", path, page+1))
		}
		context.Response().Header().Set("Link", strings.Join(links, ","))
	}

	return context.JSON(http.StatusOK, res)
}

func (h *handlers) GetAnnouncementDetail(context echo.Context) error {
	sess, err := session.Get(SessionName, context)
	if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}
	userID := uuid.Parse(sess.Values["userID"].(string))
	if userID == nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	announcementID := uuid.Parse(context.Param("announcementID"))
	if announcementID == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid announcementID")
	}

	var announcement Announcement
	if err := h.DB.Get(&announcement, "SELECT `announcements`.`id`, `courses`.`name`, `announcements`.`title`, `announcements`.`message`, `announcements`.`created_at`"+
		"FROM `announcements`"+
		"JOIN `courses` ON `announcements`.`course_id` = `courses`.`id`"+
		"JOIN `registrations` ON `announcements`.`course_id` = `registrations`.`course_id`"+
		"WHERE `announcements`.`id` = ? AND `registrations`.`user_id` = ? AND `registrations`.`deleted_at` IS NULL", announcementID, userID); err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "announcement not found.")
	} else if err != nil {
		log.Println(err)
		return context.NoContent(http.StatusInternalServerError)
	}

	res := GetAnnouncementDetailResponse{
		ID:         announcement.ID,
		CourseName: announcement.CourseName,
		Title:      announcement.Title,
		Message:    announcement.Message,
		CreatedAt:  announcement.CreatedAt.UnixNano() / int64(time.Millisecond),
	}
	return context.JSON(http.StatusOK, res)
}

func (h *handlers) PostAttendanceCode(c echo.Context) error {
	sess, err := session.Get(SessionName, c)
	if err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}
	userID := uuid.Parse(sess.Values["userID"].(string))
	if uuid.Equal(uuid.NIL, userID) {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	var req PostAttendanceCodeRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("bind request: %v", err))
	}

	// 出席コード確認
	var class Class
	if err := h.DB.Get(&class, "SELECT * FROM `classes` WHERE `attendance_code` = ?", req.Code); err == sql.ErrNoRows {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid code")
	} else if err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	// 学期確認
	if ok, err := h.courseIsInCurrentPhase(class.CourseID); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	} else if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid code")
	}

	// 履修確認
	var registration int
	if err := h.DB.Get(&registration, "SELECT COUNT(*) FROM `registrations` WHERE `course_id` = ? AND `user_id` = ? AND `deleted_at` IS NULL", class.CourseID, userID); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}
	if registration == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "You are not registered in the course.")
	}

	// 既に出席しているか
	var attendances int
	if err := h.DB.Get(&attendances, "SELECT COUNT(*) FROM `attendances` WHERE `class_id` = ? AND `user_id` = ?", class.ID, userID); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}
	if attendances > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "You have already attended in this class.")
	}

	// 出席コード登録
	if _, err := h.DB.Exec("INSERT INTO `attendances` (`class_id`, `user_id`, `created_at`) VALUES (?, ?, NOW(6))", class.ID, userID); err != nil {
		log.Println(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *handlers) courseIsInCurrentPhase(courseID uuid.UUID) (bool, error) {
	// MEMO: 複数phaseに渡る講義を想定していない
	var schedule Schedule
	query := "SELECT `schedules`.*" +
		"FROM `schedules`" +
		"JOIN `course_schedules` ON `schedules`.`id` = `course_schedules`.`schedule_id`" +
		"JOIN `courses` ON `course_schedules`.`course_id` = `courses`.`id`" +
		"WHERE `courses`.`id` = ?" +
		"LIMIT 1"
	if err := h.DB.Get(&schedule, query, courseID); err != nil {
		return false, err
	}

	var phase Phase
	if err := h.DB.Get(&phase, "SELECT * FROM `phase`"); err != nil {
		return false, err
	}

	return schedule.Year == phase.Year && schedule.Semester == phase.Semester, nil
}

func createSubmissionsZip(zipFilePath string, submissions []*SubmissionWithUserName) error {
	// Zipに含めるファイルの名称変更のためコピー
	// MEMO: N回 cp はやりすぎかも
	for _, submission := range submissions {
		cpCmd := exec.Command(
			"cp",
			AssignmentsDirectory+submission.ID.String(),
			AssignmentTmpDirectory+submission.UserName+"-"+submission.ID.String()+"-"+submission.Name,
		)
		if err := cpCmd.Start(); err != nil {
			return err
		}
		if err := cpCmd.Wait(); err != nil {
			return err
		}
	}

	zipArgs := make([]string, 0, len(submissions)+2)
	zipArgs = append(zipArgs, "-j", zipFilePath)
	for _, submission := range submissions {
		zipArgs = append(zipArgs, AssignmentTmpDirectory+submission.UserName+"-"+submission.ID.String()+"-"+submission.Name)
	}
	cmd := exec.Command("zip", zipArgs...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Wait()
}
