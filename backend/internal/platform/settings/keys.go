package settings

// Keys from docs/plan/01-product-rules.md §17 (D-61). TestKeysMatchMigration
// keeps this list, the seed migration and 01 §17 identical.
//
// #nosec G101 -- these are setting key names, not credentials (D-19).
const (
	// Orders and strikes
	OrderAcceptTimeoutMin        Key = "order_accept_timeout_min"         // minutes, default 10
	PickupHoldMin                Key = "pickup_hold_min"                  // minutes, default 30
	PickupExtensionMin           Key = "pickup_extension_min"             // minutes, default 10
	PartialConfirmTimeoutMin     Key = "partial_confirm_timeout_min"      // minutes, default 5
	DeliveryDispatchTimeoutMin   Key = "delivery_dispatch_timeout_min"    // minutes, default 60
	NoshowStrikes                Key = "noshow_strikes"                   // count, default 3
	NoshowWindowDays             Key = "noshow_window_days"               // days, default 30
	NoshowLimitDays              Key = "noshow_limit_days"                // days, default 14
	ShopExpiryPenaltyThresholdBp Key = "shop_expiry_penalty_threshold_bp" // bp, default 3000
	StoreStrikeRiskThreshold     Key = "store_strike_risk_threshold"      // count, default 5
	// Stock and catalogue
	StockStaleAfterHours Key = "stock_stale_after_hours" // hours, default 72
	LowStockDefault      Key = "low_stock_default"       // count, default 5
	StoreBannersMax      Key = "store_banners_max"       // count, default 3
	// Credits and payouts
	CreditMaxPerOrder                 Key = "credit_max_per_order"                   // credits, default 30
	CreditMinOrderValuePaise          Key = "credit_min_order_value_paise"           // paise, default 10000
	CreditExpiryDays                  Key = "credit_expiry_days"                     // days, default 365
	CreditOrdersPerMonth              Key = "credit_orders_per_month"                // count, default 10
	StoreCreditRedemptionsPerDay      Key = "store_credit_redemptions_per_day"       // count, default 200
	AdminAdjustDualApprovalAbovePaise Key = "admin_adjust_dual_approval_above_paise" // paise, default 50000
	// Referrals
	RefRetailerCustomerReferrerCredits Key = "ref_retailer_customer_referrer_credits" // credits, default 30
	RefRetailerCustomerRefereeCredits  Key = "ref_retailer_customer_referee_credits"  // credits, default 100
	RefCustomerCustomerReferrerCredits Key = "ref_customer_customer_referrer_credits" // credits, default 30
	RefCustomerCustomerRefereeCredits  Key = "ref_customer_customer_referee_credits"  // credits, default 100
	RefAnyShopReferrerCredits          Key = "ref_any_shop_referrer_credits"          // credits, default 30
	RefAnyShopRefereeCredits           Key = "ref_any_shop_referee_credits"           // credits, default 100
	RefQualifyingMinOrderPaise         Key = "ref_qualifying_min_order_paise"         // paise, default 10000
	RefShopQualifyingOrders            Key = "ref_shop_qualifying_orders"             // count, default 10
	RefAttributionWindowDays           Key = "ref_attribution_window_days"            // days, default 7
	// POS
	PosReversalWindowHours    Key = "pos_reversal_window_hours"      // hours, default 24
	PosOfflineLinkMaxHours    Key = "pos_offline_link_max_hours"     // hours, default 12
	PosOfflineLinksRiskPerDay Key = "pos_offline_links_risk_per_day" // count, default 20
	// Search ranking
	SearchWText        Key = "search_w_text"        // bp, default 4500
	SearchWDistance    Key = "search_w_distance"    // bp, default 3000
	SearchWReliability Key = "search_w_reliability" // bp, default 1000
	SearchWRating      Key = "search_w_rating"      // bp, default 1000
	SearchWOffer       Key = "search_w_offer"       // bp, default 500
	// Demand insights and coach
	InsightNoSaleDays          Key = "insight_no_sale_days"           // days, default 21
	InsightMinDaysListed       Key = "insight_min_days_listed"        // days, default 21
	InsightExpiringWithinDays  Key = "insight_expiring_within_days"   // days, default 5
	InsightWantedNearbyRadiusM Key = "insight_wanted_nearby_radius_m" // meters, default 2000
	InsightMinSearches         Key = "insight_min_searches"           // count, default 10
	InsightMinSalesLines       Key = "insight_min_sales_lines"        // count, default 20
	InsightMinUnitsSellingFast Key = "insight_min_units_selling_fast" // count, default 5
	InsightMinEventsForPercent Key = "insight_min_events_for_percent" // count, default 20
	InsightPeriodDays          Key = "insight_period_days"            // days, default 7
	CoachMaxCardsPerDay        Key = "coach_max_cards_per_day"        // count, default 5
	CoachMaxPushPerDay         Key = "coach_max_push_per_day"         // count, default 1
	CoachDismissQuietDays      Key = "coach_dismiss_quiet_days"       // days, default 7
	// Field agents
	FieldAgentEditWindowDays Key = "field_agent_edit_window_days" // days, default 30
	// Feature flags
	FeatureDeliveryEnabled Key = "feature_delivery_enabled" // bool, default true
	FeatureBillQrEnabled   Key = "feature_bill_qr_enabled"  // bool, default true
	FeatureVoiceEnabled    Key = "feature_voice_enabled"    // bool, default true
	FeatureOffersEnabled   Key = "feature_offers_enabled"   // bool, default true
	FeatureBannersEnabled  Key = "feature_banners_enabled"  // bool, default true
	NotifyPushEnabled      Key = "notify_push_enabled"      // bool, default true
	NotifySmsEnabled       Key = "notify_sms_enabled"       // bool, default true
	NotifyWhatsappEnabled  Key = "notify_whatsapp_enabled"  // bool, default false
	// AI
	AiVoiceStockEnabled  Key = "ai_voice_stock_enabled"  // bool, default false
	AiBillscanEnabled    Key = "ai_billscan_enabled"     // bool, default false
	AiShelfscanEnabled   Key = "ai_shelfscan_enabled"    // bool, default false
	AiInsighttextEnabled Key = "ai_insighttext_enabled"  // bool, default false
	AiCoachEnabled       Key = "ai_coach_enabled"        // bool, default false
	AiReplydraftEnabled  Key = "ai_replydraft_enabled"   // bool, default false
	AiBannercopyEnabled  Key = "ai_bannercopy_enabled"   // bool, default false
	AiImagecheckEnabled  Key = "ai_imagecheck_enabled"   // bool, default false
	AiListparseEnabled   Key = "ai_listparse_enabled"    // bool, default false
	AiQuerynormEnabled   Key = "ai_querynorm_enabled"    // bool, default false
	AiCatalogueEnabled   Key = "ai_catalogue_enabled"    // bool, default false
	AiTriageEnabled      Key = "ai_triage_enabled"       // bool, default false
	AiRiskexplainEnabled Key = "ai_riskexplain_enabled"  // bool, default false
	AiModelFast          Key = "ai_model_fast"           // text, default claude-haiku-5-5
	AiModelSmart         Key = "ai_model_smart"          // text, default claude-sonnet-5-5
	AiDailyCallsStore    Key = "ai_daily_calls_store"    // count, default 100
	AiDailyCallsCustomer Key = "ai_daily_calls_customer" // count, default 30
	AiDailyCostCapPaise  Key = "ai_daily_cost_cap_paise" // paise, default 50000
	// Client versions
	MinClientVersionCustomer Key = "min_client_version_customer" // text, default 0.0.0
	MinClientVersionBusiness Key = "min_client_version_business" // text, default 0.0.0
	MinClientVersionAdmin    Key = "min_client_version_admin"    // text, default 0.0.0
)

// AllKeys lists every key in 01 §17 order.
var AllKeys = []Key{
	OrderAcceptTimeoutMin,
	PickupHoldMin,
	PickupExtensionMin,
	PartialConfirmTimeoutMin,
	DeliveryDispatchTimeoutMin,
	NoshowStrikes,
	NoshowWindowDays,
	NoshowLimitDays,
	ShopExpiryPenaltyThresholdBp,
	StoreStrikeRiskThreshold,
	StockStaleAfterHours,
	LowStockDefault,
	StoreBannersMax,
	CreditMaxPerOrder,
	CreditMinOrderValuePaise,
	CreditExpiryDays,
	CreditOrdersPerMonth,
	StoreCreditRedemptionsPerDay,
	AdminAdjustDualApprovalAbovePaise,
	RefRetailerCustomerReferrerCredits,
	RefRetailerCustomerRefereeCredits,
	RefCustomerCustomerReferrerCredits,
	RefCustomerCustomerRefereeCredits,
	RefAnyShopReferrerCredits,
	RefAnyShopRefereeCredits,
	RefQualifyingMinOrderPaise,
	RefShopQualifyingOrders,
	RefAttributionWindowDays,
	PosReversalWindowHours,
	PosOfflineLinkMaxHours,
	PosOfflineLinksRiskPerDay,
	SearchWText,
	SearchWDistance,
	SearchWReliability,
	SearchWRating,
	SearchWOffer,
	InsightNoSaleDays,
	InsightMinDaysListed,
	InsightExpiringWithinDays,
	InsightWantedNearbyRadiusM,
	InsightMinSearches,
	InsightMinSalesLines,
	InsightMinUnitsSellingFast,
	InsightMinEventsForPercent,
	InsightPeriodDays,
	CoachMaxCardsPerDay,
	CoachMaxPushPerDay,
	CoachDismissQuietDays,
	FieldAgentEditWindowDays,
	FeatureDeliveryEnabled,
	FeatureBillQrEnabled,
	FeatureVoiceEnabled,
	FeatureOffersEnabled,
	FeatureBannersEnabled,
	NotifyPushEnabled,
	NotifySmsEnabled,
	NotifyWhatsappEnabled,
	AiVoiceStockEnabled,
	AiBillscanEnabled,
	AiShelfscanEnabled,
	AiInsighttextEnabled,
	AiCoachEnabled,
	AiReplydraftEnabled,
	AiBannercopyEnabled,
	AiImagecheckEnabled,
	AiListparseEnabled,
	AiQuerynormEnabled,
	AiCatalogueEnabled,
	AiTriageEnabled,
	AiRiskexplainEnabled,
	AiModelFast,
	AiModelSmart,
	AiDailyCallsStore,
	AiDailyCallsCustomer,
	AiDailyCostCapPaise,
	MinClientVersionCustomer,
	MinClientVersionBusiness,
	MinClientVersionAdmin,
}
