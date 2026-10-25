import { createFileRoute } from '@tanstack/react-router';
import { CustomersPage } from '../../customers/CustomersPage';

export const Route = createFileRoute('/customers/')({ component: CustomersPage });
