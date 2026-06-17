package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

func TestTeamRepoCRUD(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	repo := store.NewTeamRepo(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	team := store.Team{
		ID:           store.NewTeamID(),
		SessionID:    "sess-team",
		TenantID:     "tenant-default",
		Name:         "alpha",
		LeadThreadID: "sthr_primary",
		LeadAgentID:  "agt-lead",
		Status:       "active",
		CreatedAt:    now,
	}
	if err := repo.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetTeamByName(ctx, team.SessionID, team.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != team.ID {
		t.Fatalf("team=%v", got)
	}

	member := store.TeamMember{
		ID:          store.NewTeamMemberID(),
		TeamID:      team.ID,
		AgentID:     "agt-worker",
		DisplayName: "coder-1",
		ThreadID:    store.NewThreadID(),
		BackendType: "in_process",
		Status:      "idle",
		JoinedAt:    now,
	}
	if err := repo.CreateMember(ctx, member); err != nil {
		t.Fatal(err)
	}

	members, err := repo.ListMembers(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].DisplayName != "coder-1" {
		t.Fatalf("members=%v", members)
	}

	msg := store.AgentMessage{
		ID:           store.NewTeamMessageID(),
		TeamID:       team.ID,
		FromMemberID: member.ID,
		ToMemberID:   member.ID,
		MessageType:  "text",
		Body:         "hello",
		CreatedAt:    now,
	}
	if err := repo.CreateMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}

	unread, err := repo.ListUnreadMessages(ctx, team.ID, member.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 {
		t.Fatalf("unread=%v", unread)
	}

	n, err := repo.MarkMessagesRead(ctx, team.ID, member.ID, []string{msg.ID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("marked=%d", n)
	}

	unread, err = repo.ListUnreadMessages(ctx, team.ID, member.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected no unread, got %v", unread)
	}
}
