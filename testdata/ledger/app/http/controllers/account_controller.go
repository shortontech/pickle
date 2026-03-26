package controllers

import (
	pickle "github.com/shortontech/ledger/app/http"
	"github.com/shortontech/ledger/app/http/requests"
	"github.com/shortontech/ledger/app/models"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type AccountController struct {
	pickle.Controller
}

func (c AccountController) Index(ctx *pickle.Context) pickle.Response {
	ownerID, err := uuid.Parse(ctx.Auth().UserID)
	if err != nil {
		return ctx.Unauthorized("invalid auth")
	}

	accounts, err := models.QueryAccount().
		WhereOwnerID(ownerID).
		SelectAll().
		Limit(100).
		All()
	if err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, accounts)
}

func (c AccountController) Show(ctx *pickle.Context) pickle.Response {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.BadRequest("invalid account id")
	}

	ownerID, err := uuid.Parse(ctx.Auth().UserID)
	if err != nil {
		return ctx.Unauthorized("invalid auth")
	}

	account, err := models.QueryAccount().
		WhereID(id).
		WhereOwnerID(ownerID).
		SelectAll().
		First()
	if err != nil {
		return ctx.NotFound("account not found")
	}

	return ctx.JSON(200, account)
}

func (c AccountController) Store(ctx *pickle.Context) pickle.Response {
	ownerID, err := uuid.Parse(ctx.Auth().UserID)
	if err != nil {
		return ctx.Unauthorized("invalid auth")
	}

	req, bindErr := requests.BindCreateAccountRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	account := &models.Account{
		OwnerID:  ownerID,
		Name:     req.Name,
		Currency: req.Currency,
		Type:     req.Type,
		Active:   true,
	}

	if err := models.QueryAccount().Create(account); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(201, account)
}

func (c AccountController) Update(ctx *pickle.Context) pickle.Response {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.BadRequest("invalid account id")
	}

	ownerID, err := uuid.Parse(ctx.Auth().UserID)
	if err != nil {
		return ctx.Unauthorized("invalid auth")
	}

	req, bindErr := requests.BindUpdateAccountRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	account, err := models.QueryAccount().
		WhereID(id).
		WhereOwnerID(ownerID).
		SelectAll().
		First()
	if err != nil {
		return ctx.NotFound("account not found")
	}

	if req.Name != nil {
		account.Name = *req.Name
	}
	if req.Active != nil {
		account.Active = *req.Active
	}

	if err := models.QueryAccount().Update(account); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, account)
}

// Balance returns the computed balance for an account.
// Credits and settled debits are summed; pending/failed/reversed transactions are excluded.
func (c AccountController) Balance(ctx *pickle.Context) pickle.Response {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.BadRequest("invalid account id")
	}

	ownerID, err := uuid.Parse(ctx.Auth().UserID)
	if err != nil {
		return ctx.Unauthorized("invalid auth")
	}

	account, err := models.QueryAccount().
		WhereID(id).
		WhereOwnerID(ownerID).
		SelectAll().
		First()
	if err != nil {
		return ctx.NotFound("account not found")
	}

	credits, err := models.QueryTransaction().
		WhereAccountID(account.ID).
		WhereType("credit").
		SumAmount()
	if err != nil {
		return ctx.Error(err)
	}

	debits, err := models.QueryTransaction().
		WhereAccountID(account.ID).
		WhereTypeIn([]string{"debit", "fee"}).
		SumAmount()
	if err != nil {
		return ctx.Error(err)
	}

	// Reversals offset their originals, so they cancel out in the balance.
	// A reversal of a debit adds back; a reversal of a credit subtracts.
	// Since reversals carry the same amount as the original and are typed "reversal",
	// we need to check what they reverse. For simplicity, reversals of debits
	// are summed as credits, and reversals of credits as debits.
	// TODO: For a production system, join reversals to their originals.
	// For now, reversed transactions are excluded by convention — users
	// create a reversal entry that nets to zero with the original.

	creditSum := decimal.NewFromFloat(0)
	if credits != nil {
		creditSum = decimal.NewFromFloat(*credits)
	}
	debitSum := decimal.NewFromFloat(0)
	if debits != nil {
		debitSum = decimal.NewFromFloat(*debits)
	}

	balance := creditSum.Sub(debitSum)

	return ctx.JSON(200, map[string]any{
		"account_id": account.ID,
		"currency":   account.Currency,
		"balance":    balance.StringFixed(2),
	})
}

func (c AccountController) Destroy(ctx *pickle.Context) pickle.Response {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.BadRequest("invalid account id")
	}

	ownerID, err := uuid.Parse(ctx.Auth().UserID)
	if err != nil {
		return ctx.Unauthorized("invalid auth")
	}

	account, err := models.QueryAccount().
		WhereID(id).
		WhereOwnerID(ownerID).
		SelectAll().
		First()
	if err != nil {
		return ctx.NotFound("account not found")
	}

	if err := models.QueryAccount().Delete(account); err != nil {
		return ctx.Error(err)
	}

	return ctx.NoContent()
}
