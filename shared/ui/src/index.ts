// @todayz/ui: the TodayZ React UI kit (W1-02). One component per design-system card, rendering the
// tz- classes from @todayz/tokens/styles.css plus @todayz/ui/styles.css. Every visible string
// arrives as a prop; money, distance and time arrive pre-formatted. StoreMap lives in
// @todayz/ui/map so screens without a map never bundle MapLibre.

// Inputs and actions
export {
  Button,
  IconButton,
  type ButtonProps,
  type ButtonSize,
  type ButtonVariant,
  type IconButtonProps,
} from './Button/Button';
export { TextField, type TextFieldProps } from './TextField/TextField';
export { SearchField, type SearchFieldProps } from './SearchField/SearchField';
export {
  Chip,
  ChipGroup,
  RadiusChips,
  type ChipGroupProps,
  type ChipOption,
  type ChipProps,
  type MultiChipGroupProps,
  type RadiusChipsProps,
  type SingleChipGroupProps,
} from './Chip/Chip';
export {
  QuantityStepper,
  type QuantityStepperLabels,
  type QuantityStepperProps,
} from './QuantityStepper/QuantityStepper';
export { OtpInput, type OtpInputProps } from './OtpInput/OtpInput';
export { Tabs, type TabItem, type TabsProps } from './Tabs/Tabs';
export { Segmented, type SegmentedOption, type SegmentedProps } from './Segmented/Segmented';
export { FilterButton, type FilterButtonProps } from './FilterButton/FilterButton';
export { LanguagePicker, type LanguageOption, type LanguagePickerProps } from './LanguagePicker/LanguagePicker';

// Status
export { StatusPill, type StatusPillProps } from './StatusPill/StatusPill';
export { orderStatusTone, type OrderStatus, type PillTone } from './StatusPill/orderStatus';
export { StockTag, type StockLevel, type StockTagProps } from './StockTag/StockTag';

// Commerce
export { ProductCard, type ProductCardProps } from './ProductCard/ProductCard';
export { StoreCard, type StoreCardProps, type StoreTag, type Tint } from './StoreCard/StoreCard';
export { CategoryRow, CategoryTile, type CategoryRowProps, type CategoryTileProps } from './CategoryTile/CategoryTile';
export { CartBar, type CartBarProps } from './CartBar/CartBar';
export { PickupCode, type PickupCodeProps } from './PickupCode/PickupCode';
export {
  OrderTimeline,
  type OrderTimelineProps,
  type StepState,
  type TimelineStep,
} from './OrderTimeline/OrderTimeline';
export { WalletCard, type WalletCardProps } from './WalletCard/WalletCard';
export { PosCart, type PosCartProps, type PosLine } from './PosCart/PosCart';

// Dashboard
export {
  KpiStrip,
  KpiTile,
  type KpiDelta,
  type KpiProps,
  type KpiStripProps,
  type KpiTileProps,
} from './KpiTile/KpiTile';
export { DataTable, type ColumnDef, type DataTableProps, type SortingState } from './DataTable/DataTable';
export { DemandInsights, type DemandInsightsProps } from './DemandInsights/DemandInsights';
export {
  INSIGHT_LOOK,
  canDismissInsight,
  pickInsightRows,
  type InsightKind,
  type InsightRow,
} from './DemandInsights/insights';

// Navigation
export { Sidebar, type NavGroup, type NavItem, type SidebarProps } from './Sidebar/Sidebar';
export { BottomNav, type BottomNavProps } from './BottomNav/BottomNav';

// Feedback and overlays
export { Banner, type BannerProps } from './Banner/Banner';
export { Sheet, type SheetProps } from './Sheet/Sheet';
export { Dialog, type DialogProps } from './Dialog/Dialog';
export { Toast, ToastProvider, type ToastProviderProps } from './Toast/Toast';
export { useToast, type ShowToast, type ToastAction, type ToastApi, type ToastProps } from './Toast/context';
export { Skeleton, type SkeletonProps } from './Skeleton/Skeleton';
export { EmptyState, type EmptyStateProps } from './EmptyState/EmptyState';

// Shared helpers
export { VisuallyHidden } from './internal/VisuallyHidden';
export type { LucideIcon } from './internal/Icon';
export type { LinkComponent, LinkProps } from './internal/link';
