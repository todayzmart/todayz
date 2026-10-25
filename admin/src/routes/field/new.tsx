import { createFileRoute } from '@tanstack/react-router';
import { NewShopPage } from '../../field/NewShopPage';

export const Route = createFileRoute('/field/new')({ component: NewShopPage });
