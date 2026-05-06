// Adapter shim: lets call sites continue to use shadcn-style flat exports
// (Dialog/DialogContent/DialogHeader/DialogTitle/DialogDescription/DialogFooter/
//  DialogTrigger/DialogClose) on top of datum-ui's compound Dialog.X API.
//
// datum-ui exposes Dialog as a function plus Dialog.Trigger/Content/Header/
// Body/Footer/Overlay. This shim re-shapes those into the named exports our
// existing components import.
import * as React from 'react';
import { Dialog as DUIDialog } from '@datum-cloud/datum-ui/dialog';

interface DialogProps {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  defaultOpen?: boolean;
  children: React.ReactNode;
}

function Dialog(props: DialogProps) {
  return <DUIDialog {...props} />;
}

interface DialogTriggerProps {
  children: React.ReactNode;
  asChild?: boolean;
}

function DialogTrigger({ children, asChild }: DialogTriggerProps) {
  return <DUIDialog.Trigger asChild={asChild}>{children}</DUIDialog.Trigger>;
}

interface DialogContentProps {
  children: React.ReactNode;
  className?: string;
}

function DialogContent({ children, className }: DialogContentProps) {
  // datum-ui's Dialog.Content wraps the body. Header/Footer must be siblings
  // inside it, but the legacy API put DialogHeader/Body/Footer as direct
  // siblings of DialogContent. We wrap them all in a single Content node.
  return <DUIDialog.Content className={className}>{children}</DUIDialog.Content>;
}

// Legacy flat exports that map to the compound's pieces. They render their
// children inside a Body/Header-equivalent container.
function DialogHeader({ children, className }: { children?: React.ReactNode; className?: string }) {
  // Legacy DialogHeader contained DialogTitle + DialogDescription children;
  // datum-ui's Dialog.Header takes structured `title`/`description` props.
  // To keep call-site compatibility we render the children verbatim and
  // rely on DialogTitle/Description below to style them.
  return <div className={className}>{children}</div>;
}

function DialogTitle({ children, className }: { children?: React.ReactNode; className?: string }) {
  return <h2 className={className ?? 'text-lg font-semibold'}>{children}</h2>;
}

function DialogDescription({ children, className }: { children?: React.ReactNode; className?: string }) {
  return <p className={className ?? 'text-sm text-muted-foreground'}>{children}</p>;
}

function DialogBody({ children, className }: { children?: React.ReactNode; className?: string }) {
  return <DUIDialog.Body className={className}>{children}</DUIDialog.Body>;
}

function DialogFooter({ children, className }: { children?: React.ReactNode; className?: string }) {
  return <DUIDialog.Footer className={className}>{children}</DUIDialog.Footer>;
}

function DialogClose({ children }: { children?: React.ReactNode }) {
  return <>{children}</>;
}

function DialogOverlay({ className }: { className?: string }) {
  return <DUIDialog.Overlay className={className} />;
}
function DialogPortal({ children }: { children?: React.ReactNode }) {
  return <>{children}</>;
}

export {
  Dialog,
  DialogBody,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
};
