import type { ReactNode } from 'react';
import { Dialog, DialogContent, DialogTitle, IconButton, Stack } from '@mui/material';
import CloseIcon from '@mui/icons-material/Close';

interface ModalProps {
  open: boolean;
  title: string;
  onClose: () => void;
  headerExtra?: ReactNode;
  /** "auto" (default) sizes the modal to its content, up to 88vh — used by
   * every modal except the map Preview, which needs a fixed, tall canvas
   * for MapLibre. */
  variant?: 'auto' | 'full';
  noPadding?: boolean;
  children: ReactNode;
}

export default function Modal({
  open,
  title,
  onClose,
  headerExtra,
  variant = 'auto',
  noPadding,
  children,
}: ModalProps) {
  return (
    <Dialog open={open} onClose={onClose} maxWidth="lg" fullWidth fullScreen={variant === 'full'}>
      <DialogTitle
        sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 2 }}
      >
        {title}
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          {headerExtra}
          <IconButton aria-label="Close" onClick={onClose} size="small">
            <CloseIcon fontSize="small" />
          </IconButton>
        </Stack>
      </DialogTitle>
      <DialogContent
        dividers
        sx={
          noPadding
            ? { p: 0, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }
            : undefined
        }
      >
        {children}
      </DialogContent>
    </Dialog>
  );
}
