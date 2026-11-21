//go:build !postgres_test

package router

import (
	"testing"
	"time"

	"github.com/naiba/solitudes/internal/model"
)

// A guest types any address into the comment form. Notifying that address
// about replies let anyone make the site send mail, with text they wrote, to
// people who never used it. Only an address a registered account has verified
// may receive a reply notification.
func TestReplyNotificationRecipientRequiresVerifiedAccountEmail(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	now := time.Now()
	accounts := []model.Account{
		{ID: "10000000-0000-4000-8000-0000000000a1", Email: "verified@example.com", Nickname: "Verified", Role: model.RoleUser, EmailVerifiedAt: &now},
		{ID: "10000000-0000-4000-8000-0000000000a2", Email: "unverified@example.com", Nickname: "Unverified", Role: model.RoleUser},
		{ID: "10000000-0000-4000-8000-0000000000a3", Email: "disabled@example.com", Nickname: "Disabled", Role: model.RoleUser, EmailVerifiedAt: &now, DisabledAt: &now},
	}
	for index := range accounts {
		if err := db.Create(&accounts[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	commentBy := func(accountID *string, typedEmail string) *model.Comment {
		return &model.Comment{ID: "parent", Nickname: "Author", Content: "Parent comment", Email: typedEmail, AccountID: accountID}
	}

	if recipient := replyNotificationRecipient(commentBy(nil, "stranger@example.com")); recipient != nil {
		t.Errorf("a guest comment's typed address %q would be notified", recipient.Email)
	}
	if recipient := replyNotificationRecipient(commentBy(&accounts[1].ID, "unverified@example.com")); recipient != nil {
		t.Errorf("an unverified account address %q would be notified", recipient.Email)
	}
	if recipient := replyNotificationRecipient(commentBy(&accounts[2].ID, "disabled@example.com")); recipient != nil {
		t.Errorf("a disabled account's address %q would be notified", recipient.Email)
	}

	// The address stored with the comment may be outdated or forged; the
	// account's current verified address is the one to use.
	parent := commentBy(&accounts[0].ID, "address-at-comment-time@example.com")
	recipient := replyNotificationRecipient(parent)
	if recipient == nil || recipient.Email != "verified@example.com" {
		t.Fatalf("recipient for a verified account = %+v, want verified@example.com", recipient)
	}
	if recipient.Content != "Parent comment" || recipient.Nickname != "Author" {
		t.Errorf("recipient lost the quoted comment: %+v", recipient)
	}
	if parent.Email != "address-at-comment-time@example.com" {
		t.Error("looking up the recipient changed the stored comment")
	}
}

// A reply to one's own comment is not notified. Whether it is one's own is a
// matter of the account: a guest who typed the recipient's address into the
// form used to count as the recipient and silenced the notification.
func TestCommentAuthorIsRecipientComparesAccountsNotTypedAddresses(t *testing.T) {
	recipientAccount, otherAccount := "10000000-0000-4000-8000-0000000000b1", "10000000-0000-4000-8000-0000000000b2"
	recipient := &model.Comment{Email: "recipient@example.com", AccountID: &recipientAccount}
	sameAccount := recipientAccount

	if commentAuthorIsRecipient(&model.Comment{Email: "recipient@example.com"}, recipient) {
		t.Error("a guest who typed the recipient's address was taken for the recipient")
	}
	if commentAuthorIsRecipient(&model.Comment{Email: "recipient@example.com", AccountID: &otherAccount}, recipient) {
		t.Error("another account was taken for the recipient")
	}
	if !commentAuthorIsRecipient(&model.Comment{Email: "changed@example.com", AccountID: &sameAccount}, recipient) {
		t.Error("the recipient replying to their own comment must not be notified")
	}
}
