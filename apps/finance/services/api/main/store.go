package main

import (
	"context"
	"fmt"

	"homelab/pkg/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	mgmongo "go.mongodb.org/mongo-driver/v2/mongo"
)

type Store struct {
	db *mongo.DB
}

func NewStore(db *mongo.DB) *Store {
	return &Store{db: db}
}

func (s *Store) accounts() *mgmongo.Collection {
	return s.db.Collection("finance_accounts")
}

func (s *Store) categories() *mgmongo.Collection {
	return s.db.Collection("finance_categories")
}

func (s *Store) transactions() *mgmongo.Collection {
	return s.db.Collection("finance_transactions")
}

func (s *Store) trades() *mgmongo.Collection {
	return s.db.Collection("finance_trades")
}

func (s *Store) permissions() *mgmongo.Collection {
	return s.db.Collection("finance_permissions")
}

func (s *Store) getAccounts(ctx context.Context, userID string) ([]Account, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getAccounts")
	defer span.End()
	cur, err := s.accounts().Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("find accounts: %w", err)
	}
	defer cur.Close(ctx)
	var accounts []Account
	if err := cur.All(ctx, &accounts); err != nil {
		return nil, fmt.Errorf("decode accounts: %w", err)
	}
	return accounts, nil
}

func (s *Store) getAccount(ctx context.Context, id string) (*Account, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getAccount")
	defer span.End()
	var a Account
	if err := s.accounts().FindOne(ctx, bson.M{"_id": id}).Decode(&a); err != nil {
		return nil, fmt.Errorf("find account: %w", err)
	}
	return &a, nil
}

func (s *Store) createAccount(ctx context.Context, a *Account) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createAccount")
	defer span.End()
	_, err := s.accounts().InsertOne(ctx, a)
	return err
}

func (s *Store) deleteAccount(ctx context.Context, id, userID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteAccount")
	defer span.End()
	_, err := s.accounts().DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	return err
}

func (s *Store) getCategories(ctx context.Context, userID string) ([]Category, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getCategories")
	defer span.End()
	cur, err := s.categories().Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("find categories: %w", err)
	}
	defer cur.Close(ctx)
	var cats []Category
	if err := cur.All(ctx, &cats); err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}
	return cats, nil
}

func (s *Store) createCategory(ctx context.Context, c *Category) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createCategory")
	defer span.End()
	_, err := s.categories().InsertOne(ctx, c)
	return err
}

func (s *Store) updateCategory(ctx context.Context, c *Category) error {
	ctx, span := mongo.StartSpan(ctx, "Store.updateCategory")
	defer span.End()
	_, err := s.categories().UpdateOne(ctx, bson.M{"_id": c.ID, "user_id": c.UserID}, bson.M{"$set": bson.M{
		"name":        c.Name,
		"color":       c.Color,
		"budget_cents": c.BudgetCents,
	}})
	return err
}

func (s *Store) deleteCategory(ctx context.Context, id, userID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteCategory")
	defer span.End()
	_, err := s.categories().DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	return err
}

func (s *Store) getTransactions(ctx context.Context, userID string, filter bson.M) ([]Transaction, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getTransactions")
	defer span.End()
	q := bson.M{"user_id": userID}
	for k, v := range filter {
		q[k] = v
	}
	opts := options.Find().SetSort(bson.M{"date": -1})
	cur, err := s.transactions().Find(ctx, q, opts)
	if err != nil {
		return nil, fmt.Errorf("find transactions: %w", err)
	}
	defer cur.Close(ctx)
	var txns []Transaction
	if err := cur.All(ctx, &txns); err != nil {
		return nil, fmt.Errorf("decode transactions: %w", err)
	}
	return txns, nil
}

func (s *Store) getTransaction(ctx context.Context, id, userID string) (*Transaction, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getTransaction")
	defer span.End()
	var t Transaction
	if err := s.transactions().FindOne(ctx, bson.M{"_id": id, "user_id": userID}).Decode(&t); err != nil {
		return nil, fmt.Errorf("find transaction: %w", err)
	}
	return &t, nil
}

func (s *Store) createTransactions(ctx context.Context, txns []Transaction) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createTransactions")
	defer span.End()
	docs := make([]interface{}, len(txns))
	for i := range txns {
		docs[i] = txns[i]
	}
	_, err := s.transactions().InsertMany(ctx, docs)
	return err
}

func (s *Store) updateTransaction(ctx context.Context, id, userID string, update bson.M) error {
	ctx, span := mongo.StartSpan(ctx, "Store.updateTransaction")
	defer span.End()
	_, err := s.transactions().UpdateOne(ctx, bson.M{"_id": id, "user_id": userID}, bson.M{"$set": update})
	return err
}

func (s *Store) deleteTransaction(ctx context.Context, id, userID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteTransaction")
	defer span.End()
	_, err := s.transactions().DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	return err
}

func (s *Store) aggregateTransactions(ctx context.Context, userID string, pipeline bson.A) ([]bson.M, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.aggregateTransactions")
	defer span.End()
	pipeline = append(bson.A{bson.M{"$match": bson.M{"user_id": userID}}}, pipeline...)
	cur, err := s.transactions().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var results []bson.M
	if err := cur.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Store) getTrades(ctx context.Context, userID string) ([]Trade, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getTrades")
	defer span.End()
	opts := options.Find().SetSort(bson.M{"date": 1})
	cur, err := s.trades().Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find trades: %w", err)
	}
	defer cur.Close(ctx)
	var trades []Trade
	if err := cur.All(ctx, &trades); err != nil {
		return nil, fmt.Errorf("decode trades: %w", err)
	}
	return trades, nil
}

func (s *Store) createTrades(ctx context.Context, trades []Trade) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createTrades")
	defer span.End()
	docs := make([]interface{}, len(trades))
	for i := range trades {
		docs[i] = trades[i]
	}
	_, err := s.trades().InsertMany(ctx, docs)
	return err
}

func (s *Store) deleteTrade(ctx context.Context, id, userID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteTrade")
	defer span.End()
	_, err := s.trades().DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	return err
}

func (s *Store) getPermissions(ctx context.Context, ownerID string) ([]Permission, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getPermissions")
	defer span.End()
	cur, err := s.permissions().Find(ctx, bson.M{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var perms []Permission
	if err := cur.All(ctx, &perms); err != nil {
		return nil, err
	}
	return perms, nil
}

func (s *Store) getGrantedViewers(ctx context.Context, viewerID string) ([]Permission, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getGrantedViewers")
	defer span.End()
	cur, err := s.permissions().Find(ctx, bson.M{"viewer_id": viewerID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var perms []Permission
	if err := cur.All(ctx, &perms); err != nil {
		return nil, err
	}
	return perms, nil
}

func (s *Store) createPermission(ctx context.Context, p *Permission) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createPermission")
	defer span.End()
	_, err := s.permissions().InsertOne(ctx, p)
	return err
}

func (s *Store) goals() *mgmongo.Collection {
	return s.db.Collection("finance_goals")
}

func (s *Store) getGoals(ctx context.Context, userID string) ([]Goal, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getGoals")
	defer span.End()
	opts := options.Find().SetSort(bson.M{"created_at": 1})
	cur, err := s.goals().Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find goals: %w", err)
	}
	defer cur.Close(ctx)
	var goals []Goal
	if err := cur.All(ctx, &goals); err != nil {
		return nil, fmt.Errorf("decode goals: %w", err)
	}
	return goals, nil
}

func (s *Store) createGoal(ctx context.Context, g *Goal) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createGoal")
	defer span.End()
	_, err := s.goals().InsertOne(ctx, g)
	return err
}

func (s *Store) updateGoal(ctx context.Context, id, userID string, update bson.M) error {
	ctx, span := mongo.StartSpan(ctx, "Store.updateGoal")
	defer span.End()
	_, err := s.goals().UpdateOne(ctx, bson.M{"_id": id, "user_id": userID}, bson.M{"$set": update})
	return err
}

func (s *Store) deleteGoal(ctx context.Context, id, userID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteGoal")
	defer span.End()
	_, err := s.goals().DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	return err
}

func (s *Store) deletePermission(ctx context.Context, ownerID, viewerID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deletePermission")
	defer span.End()
	_, err := s.permissions().DeleteOne(ctx, bson.M{"owner_id": ownerID, "viewer_id": viewerID})
	return err
}

var defaultCategories = []struct {
	Name  string
	Color string
}{
	{"Food", "#FF6384"},
	{"Groceries", "#36A2EB"},
	{"Games", "#FFCE56"},
	{"Clothing", "#4BC0C0"},
	{"Housing", "#9966FF"},
	{"Transport", "#FF9F40"},
	{"Health", "#C9CBCF"},
	{"Income", "#00E676"},
	{"Savings", "#651FFF"},
	{"Investments", "#FF6F00"},
	{"Entertainment", "#E91E63"},
	{"Utilities", "#607D8B"},
	{"Education", "#3F51B5"},
	{"Other", "#9E9E9E"},
}

func (s *Store) tickerMappings() *mgmongo.Collection {
	return s.db.Collection("finance_ticker_mappings")
}

func (s *Store) getTickerMappings(ctx context.Context, userID string) ([]TickerMapping, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getTickerMappings")
	defer span.End()
	cur, err := s.tickerMappings().Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var items []TickerMapping
	if err := cur.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) saveTickerMapping(ctx context.Context, userID, isin, ticker string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.saveTickerMapping")
	defer span.End()
	_, err := s.tickerMappings().UpdateOne(ctx,
		bson.M{"_id": isin, "user_id": userID},
		bson.M{"$set": bson.M{"ticker": ticker, "user_id": userID}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (s *Store) households() *mgmongo.Collection {
	return s.db.Collection("finance_households")
}

func (s *Store) importSchedules() *mgmongo.Collection {
	return s.db.Collection("finance_import_schedules")
}

func (s *Store) getHousehold(ctx context.Context, userID string) (*Household, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getHousehold")
	defer span.End()
	var h Household
	err := s.households().FindOne(ctx, bson.M{"$or": bson.A{
		bson.M{"owner_id": userID},
		bson.M{"partner_id": userID},
	}}).Decode(&h)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (s *Store) createHousehold(ctx context.Context, h *Household) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createHousehold")
	defer span.End()
	_, err := s.households().InsertOne(ctx, h)
	return err
}

func (s *Store) deleteHousehold(ctx context.Context, userID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteHousehold")
	defer span.End()
	_, err := s.households().DeleteOne(ctx, bson.M{"$or": bson.A{
		bson.M{"owner_id": userID},
		bson.M{"partner_id": userID},
	}})
	return err
}

func (s *Store) getImportSchedules(ctx context.Context, userID string) ([]ImportSchedule, error) {
	ctx, span := mongo.StartSpan(ctx, "Store.getImportSchedules")
	defer span.End()
	cur, err := s.importSchedules().Find(ctx, bson.M{"user_id": userID})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var items []ImportSchedule
	if err := cur.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) createImportSchedule(ctx context.Context, sched *ImportSchedule) error {
	ctx, span := mongo.StartSpan(ctx, "Store.createImportSchedule")
	defer span.End()
	_, err := s.importSchedules().InsertOne(ctx, sched)
	return err
}

func (s *Store) deleteImportSchedule(ctx context.Context, id, userID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.deleteImportSchedule")
	defer span.End()
	_, err := s.importSchedules().DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	return err
}

func (s *Store) seedCategories(ctx context.Context, userID string) error {
	ctx, span := mongo.StartSpan(ctx, "Store.seedCategories")
	defer span.End()
	count, err := s.categories().CountDocuments(ctx, bson.M{"user_id": userID})
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, dc := range defaultCategories {
		cat := &Category{
			ID:     bson.NewObjectID().Hex(),
			UserID: userID,
			Name:   dc.Name,
			Color:  dc.Color,
		}
		if _, err := s.categories().InsertOne(ctx, cat); err != nil {
			return fmt.Errorf("seed category %s: %w", dc.Name, err)
		}
	}
	return nil
}
