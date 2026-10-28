import { createFileRoute } from '@tanstack/react-router';
import { ShopChecksPage } from '../../shop-checks/ShopChecksPage';

export const Route = createFileRoute('/shop-checks/')({ component: ShopChecksPage });
