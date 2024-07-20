package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Loan struct {
	ID                  uint   `gorm:"primaryKey"`
	LoanID              string `gorm:"unique"`
	BorrowerID          string
	PrincipalAmount     float64
	InterestRate        float64
	TermWeeks           int
	WeeklyPaymentAmount float64
	OutstandingAmount   float64
	Payments            []Payment `gorm:"foreignKey:LoanID"`
	Delinquent          bool
}

type Payment struct {
	ID     uint `gorm:"primaryKey"`
	LoanID string
	Week   int
	Amount float64
	Paid   bool
}

var db *gorm.DB

func initDB() {
	var err error
	db, err = gorm.Open(sqlite.Open("loans.db"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	db.AutoMigrate(&Loan{}, &Payment{})
}

func NewLoan(loanID, borrowerID string, principalAmount, interestRate float64, termWeeks int) *Loan {
	totalAmount := principalAmount * (1 + interestRate)
	weeklyPayment := totalAmount / float64(termWeeks)
	payments := make([]Payment, termWeeks)

	for i := 0; i < termWeeks; i++ {
		payments[i] = Payment{
			LoanID: loanID,
			Week:   i + 1,
			Amount: weeklyPayment,
			Paid:   false,
		}
	}

	return &Loan{
		LoanID:              loanID,
		BorrowerID:          borrowerID,
		PrincipalAmount:     principalAmount,
		InterestRate:        interestRate,
		TermWeeks:           termWeeks,
		WeeklyPaymentAmount: weeklyPayment,
		OutstandingAmount:   totalAmount,
		Payments:            payments,
		Delinquent:          false,
	}
}

func (loan *Loan) GetOutstanding() float64 {
	return loan.OutstandingAmount
}

func (loan *Loan) IsDelinquent() bool {
	unpaidWeeks := 0
	for i := len(loan.Payments) - 1; i >= 0; i-- {
		if !loan.Payments[i].Paid {
			unpaidWeeks++
		} else {
			break
		}
		if unpaidWeeks >= 2 {
			loan.Delinquent = true
			return true
		}
	}
	loan.Delinquent = false
	return false
}

func (loan *Loan) MakePayment(amount float64) {
	for i := 0; i < len(loan.Payments); i++ {
		if !loan.Payments[i].Paid {
			if amount == loan.WeeklyPaymentAmount {
				loan.Payments[i].Paid = true
				loan.OutstandingAmount -= amount
				db.Save(&loan.Payments[i]) // Save payment to DB
				db.Save(loan)              // Update loan in DB
			}
			break
		}
	}
	loan.IsDelinquent() // Update delinquency status after payment
}

func main() {
	initDB()
	r := gin.Default()

	r.POST("/loans", func(c *gin.Context) {
		var loanData struct {
			BorrowerID      string  `json:"borrowerId"`
			PrincipalAmount float64 `json:"principalAmount"`
			InterestRate    float64 `json:"interestRate"`
			TermWeeks       int     `json:"termWeeks"`
		}
		if err := c.BindJSON(&loanData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var count int64
		results := db.Model(&Loan{}).Count(&count)
		if results.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": results.Error.Error()})
			return
		}

		loanID := fmt.Sprintf("%d", count+1)
		loan := NewLoan(loanID, loanData.BorrowerID, loanData.PrincipalAmount, loanData.InterestRate, loanData.TermWeeks)

		result := db.Create(loan)
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
			return
		}

		for _, payment := range loan.Payments {
			db.Create(&payment)
		}

		c.JSON(http.StatusCreated, loan)
	})

	r.GET("/loans/:loanId/outstanding", func(c *gin.Context) {
		loanID := c.Param("loanId")

		var loan Loan
		result := db.Preload("Payments").Where("loan_id = ?", loanID).First(&loan)
		if result.Error != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Loan not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"outstandingAmount": loan.GetOutstanding()})
	})

	r.GET("/loans/:loanId/delinquent", func(c *gin.Context) {
		loanID := c.Param("loanId")

		var loan Loan
		result := db.Preload("Payments").Where("loan_id = ?", loanID).First(&loan)
		if result.Error != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Loan not found"})
			return
		}

		delinquent := loan.IsDelinquent()
		c.JSON(http.StatusOK, gin.H{"delinquent": delinquent})
	})

	r.POST("/loans/:loanId/payment", func(c *gin.Context) {
		loanID := c.Param("loanId")
		var paymentData struct {
			Amount float64 `json:"amount"`
		}
		if err := c.BindJSON(&paymentData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var loan Loan
		result := db.Preload("Payments").Where("loan_id = ?", loanID).First(&loan)
		if result.Error != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Loan not found"})
			return
		}
		if loan.Payments[0].Amount != paymentData.Amount {
			c.JSON(http.StatusOK, gin.H{"error": "you have to pay exact amount", "amount": loan.Payments[0].Amount})
			return
		}

		loan.MakePayment(paymentData.Amount)
		db.Save(&loan)

		c.JSON(http.StatusOK, gin.H{"outstandingAmount": loan.GetOutstanding()})
	})

	r.GET("/loans/total", func(c *gin.Context) {
		var count int64
		result := db.Model(&Loan{}).Count(&count)
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"totalLoans": count})
	})

	r.Run(":8080")
}
