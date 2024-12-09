package frontend

import (
	"fmt"
	"sync"
	"time"
)

// MaxQueryBudget 500 seconds is the default and max query budget for the tenant
// const MaxQueryBudget = 500
const MaxQueryBudget = 10 // set it low for hackathon demo

// DefaultAddBudgetOnRefresh 60 seconds is the default budget added to each tenant at the refresh interval
const DefaultAddBudgetOnRefresh = 10

const DefaultRefreshInterval = 10 * time.Second

type QueryBudget struct {
	// budgetLeft is the number of seconds left for the tenant to issue queries
	budgetLeft  float64
	lastUpdated time.Time
}

type QueryBudgetManager struct {
	// tenantID -> QueryBudget map
	budgets map[string]*QueryBudget

	// interval at which we add more budget to each tenant
	refreshInterval time.Duration
	// this is the max budget that a tenant can have
	// if the tenant is not in the map, we will add them with this budget
	// if the tenant is exceeding this budget on refresh, we will remove them from the map
	// so they can be added with the default budget, if they issue a new query
	// maxBudget int

	mtx *sync.Mutex
}

// NewQueryBudgetManager creates a new QueryBudgetManager
// QueyBudgetManager is responsible for managing the query budget for each tenant
// having a budget allows us to limit the number of query time a tenant is using in our system.
// this allows us to prevent tenants from using too much query time and affecting other tenants
// and creates a fair querying experience for all tenants
func NewQueryBudgetManager(refreshInterval time.Duration) *QueryBudgetManager {
	// if refresh interval is less than or equal to 0, we will use default refresh interval
	if refreshInterval <= time.Duration(0) {
		refreshInterval = DefaultRefreshInterval
	}

	manager := &QueryBudgetManager{
		budgets:         make(map[string]*QueryBudget),
		refreshInterval: refreshInterval,
		mtx:             &sync.Mutex{},
	}
	// refresh the metadata store at regular intervals
	go manager.refresh()

	return manager
}

func (qbm *QueryBudgetManager) GetBudgetLeft(tenantID string) float64 {
	qbm.mtx.Lock()
	defer qbm.mtx.Unlock()

	budget, ok := qbm.budgets[tenantID]
	if !ok {
		return MaxQueryBudget
	}

	return budget.budgetLeft
}

func (qbm *QueryBudgetManager) IsExhausted(tenantID string) bool {
	budget := qbm.GetBudgetLeft(tenantID)
	if budget <= 0 {
		fmt.Printf("budget exhausted for tenant %s\n", tenantID)
		return true
	}
	return false
}

func (qbm *QueryBudgetManager) SpendBudget(tenantID string, secondsUsed float64) {
	qbm.mtx.Lock()
	defer qbm.mtx.Unlock()

	fmt.Printf("spending %f seconds for tenant %s\n", secondsUsed, tenantID)

	b, ok := qbm.budgets[tenantID]
	if !ok {
		b = &QueryBudget{
			budgetLeft: MaxQueryBudget,
		}
		qbm.budgets[tenantID] = b
	}

	b.budgetLeft = b.budgetLeft - secondsUsed
	b.lastUpdated = time.Now()
}

// refresh the budget for each tenant
func (qbm *QueryBudgetManager) refresh() {
	ticker := time.NewTicker(qbm.refreshInterval)
	fmt.Printf("refreshing query budget every %s\n", qbm.refreshInterval)

	for range ticker.C {
		fmt.Println("refreshing query budget...")
		qbm.mtx.Lock()
		for tenantID, budget := range qbm.budgets {
			fmt.Printf("refreshing budget for tenant %s\n", tenantID)
			// add more budget to each tenant at the refresh interval
			budget.budgetLeft += DefaultAddBudgetOnRefresh
			budget.lastUpdated = time.Now()
			// if the tenant is exceeding the max budget, remove them from the map
			// this will allow them to be added with the default budget if they issue a new query
			if budget.budgetLeft > MaxQueryBudget {
				delete(qbm.budgets, tenantID)
			}
		}
		fmt.Println("refreshed query budget")
		fmt.Println(qbm.budgets)
		qbm.mtx.Unlock()
	}
}
