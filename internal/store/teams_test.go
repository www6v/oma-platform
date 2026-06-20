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

func TestTeamTaskRepoCRUD(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	teamRepo := store.NewTeamRepo(db)
	taskRepo := store.NewTeamTaskRepo(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	team := store.Team{
		ID:           store.NewTeamID(),
		SessionID:    "sess-tasks",
		TenantID:     "tenant-default",
		Name:         "task-test",
		LeadThreadID: "sthr_primary",
		LeadAgentID:  "agt-lead",
		Status:       "active",
		CreatedAt:    now,
	}
	if err := teamRepo.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}

	task := store.TeamTask{
		ID:        store.NewTeamTaskID(),
		TeamID:    team.ID,
		Subject:   "Write tests",
		Status:    "pending",
		Blocks:    []string{},
		BlockedBy: []string{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := taskRepo.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	got, err := taskRepo.GetTask(ctx, team.ID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Subject != "Write tests" || got.Status != "pending" {
		t.Fatalf("get task: %v", got)
	}

	tasks, err := taskRepo.ListTasks(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	if err := taskRepo.UpdateTaskStatus(ctx, team.ID, task.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}
	got, err = taskRepo.GetTask(ctx, team.ID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("expected in_progress, got %s", got.Status)
	}

	deleted, err := taskRepo.DeleteTask(ctx, team.ID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected task to be deleted")
	}

	tasks, err = taskRepo.ListTasks(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after delete, got %d", len(tasks))
	}
}

func TestTeamRepoGetTeamScopedToSession(t *testing.T) {
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
		SessionID:    "sess-a",
		TenantID:     "tenant-a",
		Name:         "alpha",
		LeadThreadID: "sthr_primary",
		LeadAgentID:  "agt-lead",
		Status:       "active",
		CreatedAt:    now,
	}
	if err := repo.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetTeamByID(ctx, "sess-a", team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != team.ID {
		t.Fatalf("team=%v", got)
	}

	foreign, err := repo.GetTeamByID(ctx, "sess-b", team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foreign != nil {
		t.Fatalf("expected nil for foreign session, got %v", foreign)
	}
}
