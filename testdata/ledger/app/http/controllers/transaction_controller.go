package controllers

import (
	pickle "github.com/shortontech/ledger/app/http"
	"github.com/shortontech/ledger/app/http/requests"
	"github.com/shortontech/ledger/app/models"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransactionController struct {
	pickle.Controller
}

// verifyAccountOwnership checks that the account exists and belongs to the authenticated user.
func verifyAccountOwnership(ctx *pickle.Context) (*models.Account, pickle.Response) {
	accountID, err := uuid.Parse(ctx.Param("account_id"))
	if err != nil {
		return nil, ctx.BadRequest("invalid account id")
	}

	ownerID, err := uuid.Parse(ctx.Auth().UserID)
	if err != nil {
		return nil, ctx.Unauthorized("invalid auth")
	}

	account, err := models.QueryAccount().
		WhereID(accountID).
		WhereOwnerID(ownerID).
		SelectAll().
		First()
	if err != nil {
		return nil, ctx.NotFound("account not found")
	}

	return account, pickle.Response{}
}

func (c TransactionController) Index(ctx *pickle.Context) pickle.Response {
	account, errResp := verifyAccountOwnership(ctx)
	if errResp.StatusCode != 0 {
		return errResp
	}

	transactions, err := models.QueryTransaction().
		WhereAccountID(account.ID).
		Limit(100).
		All()
	if err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, transactions)
}

func (c TransactionController) Show(ctx *pickle.Context) pickle.Response {
	account, errResp := verifyAccountOwnership(ctx)
	if errResp.StatusCode != 0 {
		return errResp
	}

	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.BadRequest("invalid transaction id")
	}

	tx, err := models.QueryTransaction().
		WhereID(id).
		WhereAccountID(account.ID).
		First()
	if err != nil {
		return ctx.NotFound("transaction not found")
	}

	return ctx.JSON(200, tx)
}

func (c TransactionController) Store(ctx *pickle.Context) pickle.Response {
	account, errResp := verifyAccountOwnership(ctx)
	if errResp.StatusCode != 0 {
		return errResp
	}

	req, bindErr := requests.BindCreateTransactionRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return ctx.BadRequest("invalid amount")
	}

	transaction := &models.Transaction{
		AccountID:   account.ID,
		Type:        req.Type,
		Amount:      amount,
		Currency:    req.Currency,
		Description: req.Description,
	}

	if err := models.QueryTransaction().Create(transaction); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(201, transaction)
}

// Reverse creates an offsetting entry that reverses a previous transaction.
// The original transaction is never modified — this is append-only.
func (c TransactionController) Reverse(ctx *pickle.Context) pickle.Response {
	account, errResp := verifyAccountOwnership(ctx)
	if errResp.StatusCode != 0 {
		return errResp
	}

	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.BadRequest("invalid transaction id")
	}

	original, err := models.QueryTransaction().
		WhereID(id).
		WhereAccountID(account.ID).
		First()
	if err != nil {
		return ctx.NotFound("transaction not found")
	}

	// Check it hasn't already been reversed.
	existing, err := models.QueryTransaction().
		WhereReversesID(&id).
		Count()
	if err != nil {
		return ctx.Error(err)
	}
	if existing > 0 {
		return ctx.BadRequest("transaction already reversed")
	}

	desc := "reversal of " + id.String()
	reversal := &models.Transaction{
		AccountID:   account.ID,
		Type:        "reversal",
		Amount:      original.Amount,
		Currency:    original.Currency,
		Description: &desc,
		ReversesID:  &original.ID,
	}

	if err := models.QueryTransaction().Create(reversal); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(201, reversal)
}
